package store

import (
	"context"
	"fmt"
	"time"
)

// topEntriesDefaultLimit is BlogCFC's `maxrows="5"` on the dashboard
// query (admin/index.cfm).
const topEntriesDefaultLimit = 5

// TopEntriesByViews returns the most-viewed entries posted after `since`,
// highest first. It is the admin dashboard's "top entries over the past
// seven days" query (PLAN §9 A02, admin/index.cfm): like the as-is, it
// does not filter on `released`, so a draft that has been viewed in the
// admin still shows. Categories are not attached; the dashboard lists
// titles and view counts only.
func (s *Store) TopEntriesByViews(ctx context.Context, since time.Time, limit int) ([]Entry, error) {
	if limit <= 0 {
		limit = topEntriesDefaultLimit
	}
	q := "SELECT " + entryColumns + " FROM entries e WHERE e.posted > ? ORDER BY e.views DESC, e.posted DESC, e.id LIMIT ?"
	rows, err := s.db.QueryContext(ctx, q, since.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("store: top entries by views: %w", err)
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
		return nil, fmt.Errorf("store: top entries by views: %w", err)
	}
	return out, nil
}
