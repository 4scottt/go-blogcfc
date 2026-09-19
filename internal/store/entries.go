package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Entry is one blog entry. Posted is always UTC; the display zone is the
// `timezone` setting's business.
type Entry struct {
	ID            string
	Title         string
	Alias         string
	Body          string
	MoreBody      string
	Posted        time.Time
	Username      string
	AllowComments bool
	Released      bool
	Mailed        bool
	SendEmail     bool
	Views         int
	Enclosure     string
	FileSize      int64
	MimeType      string
	Summary       string
	Subtitle      string
	Keywords      string
	Duration      string

	// Categories is filled by GetEntry, GetEntryByAlias and ListEntries.
	Categories []Category
}

// Live reports whether the entry is visible to the public at t: released
// and not future-dated.
func (e *Entry) Live(t time.Time) bool {
	return e.Released && !e.Posted.After(t.UTC())
}

// EntryFilter selects and orders entries for ListEntries.
type EntryFilter struct {
	// LiveOnly restricts to released entries posted at or before now.
	LiveOnly bool
	// Released, when set, restricts to that released flag (drafts with false).
	Released *bool
	// Username restricts to one author.
	Username string
	// CategoryIDs restricts to entries in any of these categories.
	CategoryIDs []string
	// Keywords is a LIKE match over title, body and morebody.
	Keywords string
	// From and To bound `posted` (UTC), inclusive.
	From, To *time.Time
	// Sort is one of "posted", "title", "views", "username"; anything else
	// falls back to "posted".
	Sort string
	// Desc orders descending.
	Desc bool
	// Offset and Limit page the result. Limit <= 0 means no limit.
	Offset, Limit int
}

const entryColumns = `id, title, alias, body, morebody, posted, username, allowcomments,
	released, mailed, sendemail, views, enclosure, filesize, mimetype, summary,
	subtitle, keywords, duration`

var entrySortColumns = map[string]string{
	"posted":   "posted",
	"title":    "title",
	"views":    "views",
	"username": "username",
}

func (f EntryFilter) where(now time.Time) (string, []any) {
	var conds []string
	var args []any
	if f.LiveOnly {
		conds = append(conds, "e.released = 1", "e.posted <= ?")
		args = append(args, now.UTC())
	}
	if f.Released != nil {
		conds = append(conds, "e.released = ?")
		args = append(args, *f.Released)
	}
	if f.Username != "" {
		conds = append(conds, "e.username = ?")
		args = append(args, f.Username)
	}
	if len(f.CategoryIDs) > 0 {
		ph := strings.TrimSuffix(strings.Repeat("?,", len(f.CategoryIDs)), ",")
		conds = append(conds, "e.id IN (SELECT entry_id FROM entry_categories WHERE category_id IN ("+ph+"))")
		for _, id := range f.CategoryIDs {
			args = append(args, id)
		}
	}
	if kw := strings.TrimSpace(f.Keywords); kw != "" {
		like := "%" + escapeLike(kw) + "%"
		conds = append(conds, "(e.title LIKE ? OR e.body LIKE ? OR e.morebody LIKE ?)")
		args = append(args, like, like, like)
	}
	if f.From != nil {
		conds = append(conds, "e.posted >= ?")
		args = append(args, f.From.UTC())
	}
	if f.To != nil {
		conds = append(conds, "e.posted <= ?")
		args = append(args, f.To.UTC())
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

// escapeLike neutralises the LIKE wildcards in user input.
func escapeLike(s string) string {
	r := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_")
	return r.Replace(s)
}

// ListEntries returns a page of entries and the total matching the filter
// before Offset and Limit.
func (s *Store) ListEntries(ctx context.Context, f EntryFilter) ([]Entry, int, error) {
	now := time.Now().UTC()
	where, args := f.where(now)

	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM entries e"+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("store: count entries: %w", err)
	}

	col, ok := entrySortColumns[f.Sort]
	if !ok {
		col = "posted"
	}
	dir := "ASC"
	if f.Desc {
		dir = "DESC"
	}
	q := "SELECT " + entryColumns + " FROM entries e" + where + " ORDER BY e." + col + " " + dir + ", e.id " + dir
	if f.Limit > 0 {
		q += " LIMIT ? OFFSET ?"
		args = append(args, f.Limit, max(f.Offset, 0))
	} else if f.Offset > 0 {
		q += " LIMIT 18446744073709551615 OFFSET ?"
		args = append(args, f.Offset)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("store: list entries: %w", err)
	}
	defer rows.Close()

	var out []Entry
	for rows.Next() {
		var e Entry
		if err := scanEntry(rows, &e); err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("store: list entries: %w", err)
	}
	if err := s.attachCategories(ctx, out); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

type scanner interface{ Scan(dest ...any) error }

func scanEntry(sc scanner, e *Entry) error {
	var posted time.Time
	err := sc.Scan(&e.ID, &e.Title, &e.Alias, &e.Body, &e.MoreBody, &posted, &e.Username,
		&e.AllowComments, &e.Released, &e.Mailed, &e.SendEmail, &e.Views, &e.Enclosure,
		&e.FileSize, &e.MimeType, &e.Summary, &e.Subtitle, &e.Keywords, &e.Duration)
	if err != nil {
		return fmt.Errorf("store: scan entry: %w", err)
	}
	e.Posted = posted.UTC()
	return nil
}

// attachCategories fills Categories on every entry in one query.
func (s *Store) attachCategories(ctx context.Context, entries []Entry) error {
	if len(entries) == 0 {
		return nil
	}
	idx := make(map[string]*Entry, len(entries))
	args := make([]any, 0, len(entries))
	for i := range entries {
		idx[entries[i].ID] = &entries[i]
		args = append(args, entries[i].ID)
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(entries)), ",")
	rows, err := s.db.QueryContext(ctx, `SELECT ec.entry_id, c.id, c.name, c.alias
		FROM entry_categories ec JOIN categories c ON c.id = ec.category_id
		WHERE ec.entry_id IN (`+ph+`) ORDER BY c.name`, args...)
	if err != nil {
		return fmt.Errorf("store: entry categories: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var entryID string
		var c Category
		if err := rows.Scan(&entryID, &c.ID, &c.Name, &c.Alias); err != nil {
			return fmt.Errorf("store: entry categories: %w", err)
		}
		if e := idx[entryID]; e != nil {
			e.Categories = append(e.Categories, c)
		}
	}
	return rows.Err()
}

// GetEntry returns one entry by id, with its categories.
func (s *Store) GetEntry(ctx context.Context, id string) (*Entry, error) {
	return s.getEntryBy(ctx, "id", id)
}

// GetEntryByAlias returns one entry by its permalink alias.
func (s *Store) GetEntryByAlias(ctx context.Context, alias string) (*Entry, error) {
	return s.getEntryBy(ctx, "alias", alias)
}

func (s *Store) getEntryBy(ctx context.Context, col, val string) (*Entry, error) {
	var e Entry
	row := s.db.QueryRowContext(ctx, "SELECT "+entryColumns+" FROM entries e WHERE e."+col+" = ? LIMIT 1", val)
	if err := scanEntry(row, &e); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	one := []Entry{e}
	if err := s.attachCategories(ctx, one); err != nil {
		return nil, err
	}
	return &one[0], nil
}

// CreateEntry inserts an entry, filling ID when it is empty and defaulting
// Posted to now.
func (s *Store) CreateEntry(ctx context.Context, e *Entry) error {
	if e.ID == "" {
		id, err := newID()
		if err != nil {
			return err
		}
		e.ID = id
	}
	if e.Posted.IsZero() {
		e.Posted = time.Now().UTC().Truncate(time.Second)
	}
	e.Posted = utcOrZero(e.Posted)
	_, err := s.db.ExecContext(ctx, `INSERT INTO entries
		(id, title, alias, body, morebody, posted, username, allowcomments, released,
		 mailed, sendemail, views, enclosure, filesize, mimetype, summary, subtitle,
		 keywords, duration)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID, e.Title, e.Alias, e.Body, e.MoreBody, e.Posted, e.Username, e.AllowComments,
		e.Released, e.Mailed, e.SendEmail, e.Views, e.Enclosure, e.FileSize, e.MimeType,
		e.Summary, e.Subtitle, e.Keywords, e.Duration)
	if err != nil {
		if isDuplicate(err) {
			return ErrDuplicate
		}
		return fmt.Errorf("store: create entry: %w", err)
	}
	return nil
}

// UpdateEntry writes every column of an existing entry.
func (s *Store) UpdateEntry(ctx context.Context, e *Entry) error {
	e.Posted = utcOrZero(e.Posted)
	res, err := s.db.ExecContext(ctx, `UPDATE entries SET
		title = ?, alias = ?, body = ?, morebody = ?, posted = ?, username = ?,
		allowcomments = ?, released = ?, mailed = ?, sendemail = ?, views = ?,
		enclosure = ?, filesize = ?, mimetype = ?, summary = ?, subtitle = ?,
		keywords = ?, duration = ? WHERE id = ?`,
		e.Title, e.Alias, e.Body, e.MoreBody, e.Posted, e.Username, e.AllowComments,
		e.Released, e.Mailed, e.SendEmail, e.Views, e.Enclosure, e.FileSize, e.MimeType,
		e.Summary, e.Subtitle, e.Keywords, e.Duration, e.ID)
	if err != nil {
		if isDuplicate(err) {
			return ErrDuplicate
		}
		return fmt.Errorf("store: update entry: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if err := s.db.QueryRowContext(ctx, "SELECT id FROM entries WHERE id = ?", e.ID).Scan(new(string)); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
	}
	return nil
}

// DeleteEntries removes entries and everything that hangs off them:
// category links, related-entry links (both directions) and comments.
func (s *Store) DeleteEntries(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: delete entries: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op
	stmts := []string{
		"DELETE FROM entry_categories WHERE entry_id IN (" + ph + ")",
		"DELETE FROM comments WHERE entry_id IN (" + ph + ")",
		"DELETE FROM related_entries WHERE entry_id IN (" + ph + ")",
		"DELETE FROM related_entries WHERE related_id IN (" + ph + ")",
		"DELETE FROM entries WHERE id IN (" + ph + ")",
	}
	for _, q := range stmts {
		if _, err := tx.ExecContext(ctx, q, args...); err != nil {
			return fmt.Errorf("store: delete entries: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: delete entries: %w", err)
	}
	return nil
}

// SetEntryCategories replaces an entry's category set.
func (s *Store) SetEntryCategories(ctx context.Context, entryID string, catIDs []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: set entry categories: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op
	if _, err := tx.ExecContext(ctx, "DELETE FROM entry_categories WHERE entry_id = ?", entryID); err != nil {
		return fmt.Errorf("store: set entry categories: %w", err)
	}
	seen := map[string]bool{}
	for _, id := range catIDs {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		if _, err := tx.ExecContext(ctx, "INSERT INTO entry_categories (entry_id, category_id) VALUES (?, ?)", entryID, id); err != nil {
			return fmt.Errorf("store: set entry categories: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: set entry categories: %w", err)
	}
	return nil
}

// IncrementViews adds one to an entry's view counter.
func (s *Store) IncrementViews(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, "UPDATE entries SET views = views + 1 WHERE id = ?", id); err != nil {
		return fmt.Errorf("store: increment views: %w", err)
	}
	return nil
}
