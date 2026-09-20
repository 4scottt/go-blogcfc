package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Comment is one comment on an entry. A row with SubscribeOnly set is not
// a comment at all but a thread subscription (BlogCFC's addsub.cfm), and
// it stays out of every listing and count (PLAN §9 C09).
type Comment struct {
	ID      string
	EntryID string
	Name    string
	Email   string
	Website string
	Comment string

	Posted time.Time

	Subscribe     bool
	Moderated     bool
	SubscribeOnly bool

	// KillToken is the secret in the owner's one-click delete link
	// (BlogCFC's killcomment).
	KillToken string
}

// RecentComment is a comment with its entry's title, for the recent
// comments pod (PLAN §9 D05).
type RecentComment struct {
	Comment
	EntryTitle string
}

// recentCommentsDefault is BlogCFC's getRecentComments default.
const recentCommentsDefault = 10

const commentColumns = "c.id, c.entry_id, c.name, c.email, c.website, c.`comment`, " +
	"c.posted, c.subscribe, c.moderated, c.subscribeonly, c.kill_token"

func scanComment(sc scanner, c *Comment, extra ...any) error {
	var posted time.Time
	dest := []any{&c.ID, &c.EntryID, &c.Name, &c.Email, &c.Website, &c.Comment,
		&posted, &c.Subscribe, &c.Moderated, &c.SubscribeOnly, &c.KillToken}
	dest = append(dest, extra...)
	if err := sc.Scan(dest...); err != nil {
		return err
	}
	c.Posted = posted.UTC()
	return nil
}

// ListComments returns an entry's comments oldest first. Subscribe-only
// rows never appear; unmoderated ones appear only for a caller that asks
// for them (the admin and the moderation queue).
func (s *Store) ListComments(ctx context.Context, entryID string, includeUnmoderated bool) ([]Comment, error) {
	q := "SELECT " + commentColumns + " FROM comments c WHERE c.entry_id = ? AND c.subscribeonly = 0"
	if !includeUnmoderated {
		q += " AND c.moderated = 1"
	}
	q += " ORDER BY c.posted ASC, c.id ASC"
	rows, err := s.db.QueryContext(ctx, q, entryID)
	if err != nil {
		return nil, fmt.Errorf("store: list comments: %w", err)
	}
	defer rows.Close()
	var out []Comment
	for rows.Next() {
		var c Comment
		if err := scanComment(rows, &c); err != nil {
			return nil, fmt.Errorf("store: list comments: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list comments: %w", err)
	}
	return out, nil
}

// CountComments is the number an entry shows next to its comment anchor:
// moderated comments only, subscriptions excluded.
func (s *Store) CountComments(ctx context.Context, entryID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM comments WHERE entry_id = ? AND subscribeonly = 0 AND moderated = 1",
		entryID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: count comments: %w", err)
	}
	return n, nil
}

// CountCommentsFor counts a whole page of entries in one query. Entries
// with no comments are absent from the map, so a lookup gives zero.
func (s *Store) CountCommentsFor(ctx context.Context, entryIDs []string) (map[string]int, error) {
	out := map[string]int{}
	if len(entryIDs) == 0 {
		return out, nil
	}
	args := make([]any, 0, len(entryIDs))
	seen := map[string]bool{}
	for _, id := range entryIDs {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		args = append(args, id)
	}
	if len(args) == 0 {
		return out, nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(args)), ",")
	rows, err := s.db.QueryContext(ctx, `SELECT entry_id, COUNT(*) FROM comments
		WHERE entry_id IN (`+ph+`) AND subscribeonly = 0 AND moderated = 1
		GROUP BY entry_id`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: count comments for: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, fmt.Errorf("store: count comments for: %w", err)
		}
		out[id] = n
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: count comments for: %w", err)
	}
	return out, nil
}

// RecentComments returns the newest moderated comments with their entry's
// title. Only comments on live entries count: a draft's comments are not
// leaked through the sidebar.
func (s *Store) RecentComments(ctx context.Context, n int) ([]RecentComment, error) {
	if n <= 0 {
		n = recentCommentsDefault
	}
	rows, err := s.db.QueryContext(ctx, "SELECT "+commentColumns+`, e.title
		FROM comments c JOIN entries e ON e.id = c.entry_id
		WHERE c.subscribeonly = 0 AND c.moderated = 1
		AND e.released = 1 AND e.posted <= ?
		ORDER BY c.posted DESC, c.id DESC LIMIT ?`, time.Now().UTC(), n)
	if err != nil {
		return nil, fmt.Errorf("store: recent comments: %w", err)
	}
	defer rows.Close()
	var out []RecentComment
	for rows.Next() {
		var rc RecentComment
		if err := scanComment(rows, &rc.Comment, &rc.EntryTitle); err != nil {
			return nil, fmt.Errorf("store: recent comments: %w", err)
		}
		out = append(out, rc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: recent comments: %w", err)
	}
	return out, nil
}

// CreateComment inserts a comment, filling ID, KillToken and Posted when
// they are zero. Whether the comment arrives moderated is the pipeline's
// decision (PLAN §11), not the store's.
func (s *Store) CreateComment(ctx context.Context, c *Comment) error {
	if c.ID == "" {
		id, err := newID()
		if err != nil {
			return err
		}
		c.ID = id
	}
	if c.KillToken == "" {
		tok, err := newID()
		if err != nil {
			return err
		}
		c.KillToken = tok
	}
	if c.Posted.IsZero() {
		c.Posted = time.Now().UTC().Truncate(time.Second)
	}
	c.Posted = utcOrZero(c.Posted)
	_, err := s.db.ExecContext(ctx, `INSERT INTO comments
		(id, entry_id, name, email, website, `+"`comment`"+`, posted, subscribe,
		 moderated, subscribeonly, kill_token)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.EntryID, c.Name, c.Email, c.Website, c.Comment, c.Posted,
		c.Subscribe, c.Moderated, c.SubscribeOnly, c.KillToken)
	if err != nil {
		if isDuplicate(err) {
			return ErrDuplicate
		}
		return fmt.Errorf("store: create comment: %w", err)
	}
	return nil
}

// GetComment returns one comment by id.
func (s *Store) GetComment(ctx context.Context, id string) (*Comment, error) {
	return s.getCommentBy(ctx, "c.id", id)
}

// GetCommentByKillToken returns the comment a one-click delete link names.
// An empty token never matches, whatever the rows hold.
func (s *Store) GetCommentByKillToken(ctx context.Context, token string) (*Comment, error) {
	if token == "" {
		return nil, ErrNotFound
	}
	return s.getCommentBy(ctx, "c.kill_token", token)
}

func (s *Store) getCommentBy(ctx context.Context, col, val string) (*Comment, error) {
	var c Comment
	row := s.db.QueryRowContext(ctx, "SELECT "+commentColumns+" FROM comments c WHERE "+col+" = ? LIMIT 1", val)
	if err := scanComment(row, &c); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: get comment: %w", err)
	}
	return &c, nil
}

// UpdateComment writes an editable comment's columns. The entry it hangs
// off and its kill token do not move.
func (s *Store) UpdateComment(ctx context.Context, c *Comment) error {
	c.Posted = utcOrZero(c.Posted)
	res, err := s.db.ExecContext(ctx, "UPDATE comments SET name = ?, email = ?, website = ?, "+
		"`comment` = ?, posted = ?, subscribe = ?, moderated = ?, subscribeonly = ? WHERE id = ?",
		c.Name, c.Email, c.Website, c.Comment, c.Posted, c.Subscribe, c.Moderated, c.SubscribeOnly, c.ID)
	if err != nil {
		return fmt.Errorf("store: update comment: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := s.GetComment(ctx, c.ID); err != nil {
			return err
		}
	}
	return nil
}

// DeleteComments removes comments by id.
func (s *Store) DeleteComments(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	if _, err := s.db.ExecContext(ctx, "DELETE FROM comments WHERE id IN ("+ph+")", args...); err != nil {
		return fmt.Errorf("store: delete comments: %w", err)
	}
	return nil
}

// ApproveComment moderates a comment: the moderation queue's action and
// the owner's one-click approve link (PLAN §9 C11, C12).
func (s *Store) ApproveComment(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE comments SET moderated = 1 WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("store: approve comment: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := s.GetComment(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

// ClearSubscriptions turns off the subscribe flag on every earlier comment
// by that email on the entry: BlogCFC's retro-clear when a commenter posts
// again with the box unticked (PLAN §9 C08).
func (s *Store) ClearSubscriptions(ctx context.Context, entryID, email string) error {
	if email == "" {
		return nil
	}
	if _, err := s.db.ExecContext(ctx,
		"UPDATE comments SET subscribe = 0 WHERE entry_id = ? AND email = ?", entryID, email); err != nil {
		return fmt.Errorf("store: clear subscriptions: %w", err)
	}
	return nil
}

// ThreadSubscribers maps every subscribed email on an entry to one of its
// comment ids, which is what the per-recipient unsubscribe link carries
// (PLAN §11 "Notification recipients"). Subscribe-only rows count; the id
// kept for an email that subscribed more than once is its earliest.
func (s *Store) ThreadSubscribers(ctx context.Context, entryID string) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT email, id FROM comments
		WHERE entry_id = ? AND subscribe = 1 AND email <> ''
		ORDER BY posted ASC, id ASC`, entryID)
	if err != nil {
		return nil, fmt.Errorf("store: thread subscribers: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var email, id string
		if err := rows.Scan(&email, &id); err != nil {
			return nil, fmt.Errorf("store: thread subscribers: %w", err)
		}
		if _, ok := out[email]; !ok {
			out[email] = id
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: thread subscribers: %w", err)
	}
	return out, nil
}
