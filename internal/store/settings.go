package store

import (
	"context"
	"fmt"
)

// AllSettings returns the whole settings table as key/value pairs.
func (s *Store) AllSettings(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT `key`, value FROM settings")
	if err != nil {
		return nil, fmt.Errorf("store: all settings: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("store: all settings: %w", err)
		}
		out[k] = v
	}
	return out, rows.Err()
}

// SetSettings upserts the given keys, leaving the others alone.
func (s *Store) SetSettings(ctx context.Context, values map[string]string) error {
	if len(values) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: set settings: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op
	for k, v := range values {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO settings (`key`, value) VALUES (?,?) ON DUPLICATE KEY UPDATE value = VALUES(value)", k, v); err != nil {
			return fmt.Errorf("store: set settings: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: set settings: %w", err)
	}
	return nil
}
