package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// RelatedEntries returns the entries related to this one, in both
// directions: BlogCFC's getRelatedBlogEntries unions the rows that name
// this entry with the rows this entry names (PLAN §9 P13). Only live
// entries come back, newest first, with their categories attached.
func (s *Store) RelatedEntries(ctx context.Context, entryID string) ([]Entry, error) {
	if entryID == "" {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, "SELECT "+entryColumns+` FROM entries e
		WHERE e.id IN (
			SELECT related_id FROM related_entries WHERE entry_id = ?
			UNION
			SELECT entry_id FROM related_entries WHERE related_id = ?
		)
		AND e.id <> ? AND e.released = 1 AND e.posted <= ?
		ORDER BY e.posted DESC, e.id DESC`, entryID, entryID, entryID, time.Now().UTC())
	if err != nil {
		return nil, fmt.Errorf("store: related entries: %w", err)
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
		return nil, fmt.Errorf("store: related entries: %w", err)
	}
	if err := s.attachCategories(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

// SetRelatedEntries replaces the set of entries this one points at:
// delete then insert, the same shape SetEntryCategories has (PLAN §9 A09).
// The entry never relates to itself, and duplicates collapse.
func (s *Store) SetRelatedEntries(ctx context.Context, entryID string, ids []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: set related entries: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op
	if _, err := tx.ExecContext(ctx, "DELETE FROM related_entries WHERE entry_id = ?", entryID); err != nil {
		return fmt.Errorf("store: set related entries: %w", err)
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" || id == entryID || seen[id] {
			continue
		}
		seen[id] = true
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO related_entries (entry_id, related_id) VALUES (?, ?)", entryID, id); err != nil {
			return fmt.Errorf("store: set related entries: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: set related entries: %w", err)
	}
	return nil
}

// placeholders returns "?,?,?" for n arguments.
func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}
