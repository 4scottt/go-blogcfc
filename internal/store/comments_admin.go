package store

import (
	"context"
	"fmt"
	"strings"
)

// The queries the admin's comment screens need, which the public ones do
// not: a search across every entry's comments (admin/comments.cfm, which
// calls getComments with `search`), the moderation queue
// (getUnmoderatedComments) and the count the left menu carries
// (getNumberUnmoderated) — PLAN §9 A13, A14.

// CommentWithEntry is a comment with the title of the entry it hangs off,
// which is the `entrytitle` column the as-is queries select so the admin
// tables can name the entry. The entry's id is the embedded Comment's
// EntryID.
type CommentWithEntry struct {
	Comment
	EntryTitle string
}

// adminCommentSelect joins the two tables both admin lists read. The join
// is what drops a comment whose entry has gone, exactly as the as-is
// `where entryidfk = id` did.
const adminCommentSelect = "SELECT " + commentColumns + ", e.title " +
	"FROM comments c JOIN entries e ON e.id = c.entry_id"

// SearchComments returns a page of comments, newest first, with their
// entry's title and the total before paging. A non-empty q matches the
// comment text or the commenter's name, case-insensitively, as the as-is
// `like %search%` over comment and name does. Subscribe-only rows are not
// comments and never appear (PLAN §9 C09).
//
// The as-is list hid unmoderated comments whenever moderation was on
// (getComments adds `moderated = 1`), which put them out of reach of the
// very screen that edits them; this one shows both and says which is
// which.
func (s *Store) SearchComments(ctx context.Context, q string, offset, limit int) ([]CommentWithEntry, int, error) {
	where := " WHERE c.subscribeonly = 0"
	var args []any
	if q = strings.TrimSpace(q); q != "" {
		like := "%" + strings.ToLower(escapeLike(q)) + "%"
		where += " AND (LOWER(c.`comment`) LIKE ? OR LOWER(c.name) LIKE ?)"
		args = append(args, like, like)
	}

	var total int
	if err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM comments c JOIN entries e ON e.id = c.entry_id"+where, args...).
		Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("store: count comments: %w", err)
	}

	sql := adminCommentSelect + where + " ORDER BY c.posted DESC, c.id DESC"
	if limit > 0 {
		sql += " LIMIT ? OFFSET ?"
		args = append(args, limit, max(offset, 0))
	} else if offset > 0 {
		sql += " LIMIT 18446744073709551615 OFFSET ?"
		args = append(args, offset)
	}
	out, err := s.adminComments(ctx, sql, args...)
	if err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// ListUnmoderated is the moderation queue: every comment still waiting,
// newest first (PLAN §9 A14).
func (s *Store) ListUnmoderated(ctx context.Context) ([]CommentWithEntry, error) {
	return s.adminComments(ctx,
		adminCommentSelect+" WHERE c.subscribeonly = 0 AND c.moderated = 0 ORDER BY c.posted DESC, c.id DESC")
}

// CountUnmoderated is the number the left menu shows beside Moderate
// (getNumberUnmoderated).
func (s *Store) CountUnmoderated(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM comments c JOIN entries e ON e.id = c.entry_id
		WHERE c.subscribeonly = 0 AND c.moderated = 0`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: count unmoderated comments: %w", err)
	}
	return n, nil
}

func (s *Store) adminComments(ctx context.Context, query string, args ...any) ([]CommentWithEntry, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: admin comments: %w", err)
	}
	defer rows.Close()
	var out []CommentWithEntry
	for rows.Next() {
		var c CommentWithEntry
		if err := scanComment(rows, &c.Comment, &c.EntryTitle); err != nil {
			return nil, fmt.Errorf("store: admin comments: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: admin comments: %w", err)
	}
	return out, nil
}
