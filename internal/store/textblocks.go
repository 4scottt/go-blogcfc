package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Textblock is a named snippet an entry or page body pulls in with
// `<textblock label>` (PLAN §9 A22).
type Textblock struct {
	ID    string
	Label string
	Body  string
}

// ListTextblocks returns every block by label, as BlogCFC's getTextBlocks
// does.
func (s *Store) ListTextblocks(ctx context.Context) ([]Textblock, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, label, body FROM textblocks ORDER BY label ASC")
	if err != nil {
		return nil, fmt.Errorf("store: list textblocks: %w", err)
	}
	defer rows.Close()
	var out []Textblock
	for rows.Next() {
		var t Textblock
		if err := rows.Scan(&t.ID, &t.Label, &t.Body); err != nil {
			return nil, fmt.Errorf("store: list textblocks: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list textblocks: %w", err)
	}
	return out, nil
}

// GetTextblockByLabel returns one block by its label.
func (s *Store) GetTextblockByLabel(ctx context.Context, label string) (*Textblock, error) {
	var t Textblock
	err := s.db.QueryRowContext(ctx,
		"SELECT id, label, body FROM textblocks WHERE label = ? LIMIT 1", label).
		Scan(&t.ID, &t.Label, &t.Body)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get textblock: %w", err)
	}
	return &t, nil
}

// CreateTextblock inserts a block, filling ID when it is empty. A clash on
// the label is ErrDuplicate.
func (s *Store) CreateTextblock(ctx context.Context, t *Textblock) error {
	if t.ID == "" {
		id, err := newID()
		if err != nil {
			return err
		}
		t.ID = id
	}
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO textblocks (id, label, body) VALUES (?,?,?)", t.ID, t.Label, t.Body)
	if err != nil {
		if isDuplicate(err) {
			return ErrDuplicate
		}
		return fmt.Errorf("store: create textblock: %w", err)
	}
	return nil
}

// UpdateTextblock writes a block's label and body.
func (s *Store) UpdateTextblock(ctx context.Context, t *Textblock) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE textblocks SET label = ?, body = ? WHERE id = ?", t.Label, t.Body, t.ID)
	if err != nil {
		if isDuplicate(err) {
			return ErrDuplicate
		}
		return fmt.Errorf("store: update textblock: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var id string
		if err := s.db.QueryRowContext(ctx, "SELECT id FROM textblocks WHERE id = ?", t.ID).Scan(&id); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
	}
	return nil
}

// DeleteTextblock removes one block.
func (s *Store) DeleteTextblock(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, "DELETE FROM textblocks WHERE id = ?", id); err != nil {
		return fmt.Errorf("store: delete textblock: %w", err)
	}
	return nil
}

// TextblockMap is label to body for the substitution pass, in one query.
func (s *Store) TextblockMap(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT label, body FROM textblocks")
	if err != nil {
		return nil, fmt.Errorf("store: textblock map: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var label, body string
		if err := rows.Scan(&label, &body); err != nil {
			return nil, fmt.Errorf("store: textblock map: %w", err)
		}
		out[label] = body
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: textblock map: %w", err)
	}
	return out, nil
}
