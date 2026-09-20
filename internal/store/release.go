package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// The release side of an entry: the rows the one-minute sweep picks up,
// the mark it leaves behind, and the thread unsubscribe an unsubscribe
// link asks for (PLAN §9 C14, C15, C16, §11 "Release side effects").

// UnmailedReleased returns the entries whose subscriber mail is due at
// now: released, posted at or before now, `sendemail` on and not mailed
// yet. It is admin/notify.cfm's sanity checks turned into a query, and
// the sweep's whole input (PLAN §7, §9 C16).
//
// Categories are not attached: the mail carries the title, the link, the
// author and the body, and nothing else.
func (s *Store) UnmailedReleased(ctx context.Context, now time.Time) ([]Entry, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+entryColumns+` FROM entries e
		WHERE e.released = 1 AND e.posted <= ? AND e.mailed = 0 AND e.sendemail = 1
		ORDER BY e.posted ASC, e.id ASC`, now.UTC())
	if err != nil {
		return nil, fmt.Errorf("store: unmailed released entries: %w", err)
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		var e Entry
		if err := scanEntry(rows, &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: unmailed released entries: %w", err)
	}
	return out, nil
}

// MarkMailed records that an entry's subscriber mail has gone out, and to
// how many addresses. The mark is set even when count is 0: BlogCFC did
// the same and its comment admits it ("it is possible that an entry will
// never be marked mailed if your blog has no subscribers"), but the
// number goes into mailed_count so a zero-subscriber blog is not confused
// with a delivery (PLAN §7 "Bugs fixed rather than ported").
func (s *Store) MarkMailed(ctx context.Context, id string, count int) error {
	if count < 0 {
		count = 0
	}
	res, err := s.db.ExecContext(ctx,
		"UPDATE entries SET mailed = 1, mailed_count = ? WHERE id = ?", count, id)
	if err != nil {
		return fmt.Errorf("store: mark mailed: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var got string
		if err := s.db.QueryRowContext(ctx, "SELECT id FROM entries WHERE id = ?", id).Scan(&got); err != nil {
			return ErrNotFound
		}
	}
	return nil
}

// MailedCount is how many subscribers an entry's release mail reached.
func (s *Store) MailedCount(ctx context.Context, id string) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx,
		"SELECT mailed_count FROM entries WHERE id = ?", id).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: mailed count: %w", err)
	}
	return n, nil
}

// UnsubscribeThread is the `?email=&commentID=` unsubscribe link
// (unsubscribe.cfm, blog.cfc unsubscribeThread): the comment id and the
// address must agree, and then every comment that address left on the
// same entry stops being subscribed. It reports whether the pair matched;
// a pair that does not match changes nothing and is not an error, it is
// the "please check the URL" page (PLAN §9 C14).
func (s *Store) UnsubscribeThread(ctx context.Context, commentID, email string) (bool, error) {
	commentID, email = strings.TrimSpace(commentID), strings.TrimSpace(email)
	if commentID == "" || email == "" {
		return false, nil
	}
	c, err := s.GetComment(ctx, commentID)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	// The as-is matched the address in SQL, under a case-insensitive
	// collation; EqualFold keeps that and does not depend on the column's.
	if !strings.EqualFold(strings.TrimSpace(c.Email), email) {
		return false, nil
	}
	if err := s.ClearSubscriptions(ctx, c.EntryID, c.Email); err != nil {
		return false, err
	}
	return true, nil
}
