package store

import (
	"context"
	"fmt"
)

// RelatedEntriesForEditor returns every entry related to this one, in
// both directions, drafts and future entries included: what the admin's
// Related Entries tab has to show (PLAN §9 A09).
//
// It is the admin's counterpart to RelatedEntries, which is the public
// view and drops everything not live. The as-is had one function with
// three flags for this (blog.cfc getRelatedBlogEntries, called from
// admin/entry.cfm with bDisplayForAdmin true); two queries with names
// that say which side they are for read better, and the editor must not
// silently drop a relation to a draft it is about to rewrite.
func (s *Store) RelatedEntriesForEditor(ctx context.Context, entryID string) ([]Entry, error) {
	if entryID == "" {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, "SELECT "+entryColumns+` FROM entries e
		WHERE e.id IN (
			SELECT related_id FROM related_entries WHERE entry_id = ?
			UNION
			SELECT entry_id FROM related_entries WHERE related_id = ?
		)
		AND e.id <> ?
		ORDER BY e.posted DESC, e.id DESC`, entryID, entryID, entryID)
	if err != nil {
		return nil, fmt.Errorf("store: related entries for editor: %w", err)
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
		return nil, fmt.Errorf("store: related entries for editor: %w", err)
	}
	return out, nil
}
