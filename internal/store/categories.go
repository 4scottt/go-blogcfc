package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Category is a blog category. EntryCount is the number of live entries in
// it and is filled by ListCategories.
type Category struct {
	ID         string
	Name       string
	Alias      string
	EntryCount int
}

// ListCategories returns every category with its live entry count, by name.
func (s *Store) ListCategories(ctx context.Context) ([]Category, error) {
	now := time.Now().UTC()
	rows, err := s.db.QueryContext(ctx, `SELECT c.id, c.name, c.alias,
		(SELECT COUNT(*) FROM entry_categories ec JOIN entries e ON e.id = ec.entry_id
		 WHERE ec.category_id = c.id AND e.released = 1 AND e.posted <= ?) AS entrycount
		FROM categories c ORDER BY c.name`, now)
	if err != nil {
		return nil, fmt.Errorf("store: list categories: %w", err)
	}
	defer rows.Close()
	var out []Category
	for rows.Next() {
		var c Category
		if err := rows.Scan(&c.ID, &c.Name, &c.Alias, &c.EntryCount); err != nil {
			return nil, fmt.Errorf("store: list categories: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetCategory returns one category by id.
func (s *Store) GetCategory(ctx context.Context, id string) (*Category, error) {
	return s.getCategoryBy(ctx, "id", id)
}

// GetCategoryByAlias returns one category by its permalink alias.
func (s *Store) GetCategoryByAlias(ctx context.Context, alias string) (*Category, error) {
	return s.getCategoryBy(ctx, "alias", alias)
}

func (s *Store) getCategoryBy(ctx context.Context, col, val string) (*Category, error) {
	var c Category
	err := s.db.QueryRowContext(ctx, "SELECT id, name, alias FROM categories WHERE "+col+" = ? LIMIT 1", val).
		Scan(&c.ID, &c.Name, &c.Alias)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get category: %w", err)
	}
	return &c, nil
}

// CreateCategory inserts a category, filling ID when empty. A clash on the
// name or the alias is ErrDuplicate.
func (s *Store) CreateCategory(ctx context.Context, c *Category) error {
	if c.ID == "" {
		id, err := newID()
		if err != nil {
			return err
		}
		c.ID = id
	}
	_, err := s.db.ExecContext(ctx, "INSERT INTO categories (id, name, alias) VALUES (?,?,?)", c.ID, c.Name, c.Alias)
	if err != nil {
		if isDuplicate(err) {
			return ErrDuplicate
		}
		return fmt.Errorf("store: create category: %w", err)
	}
	return nil
}

// UpdateCategory writes a category's name and alias.
func (s *Store) UpdateCategory(ctx context.Context, c *Category) error {
	res, err := s.db.ExecContext(ctx, "UPDATE categories SET name = ?, alias = ? WHERE id = ?", c.Name, c.Alias, c.ID)
	if err != nil {
		if isDuplicate(err) {
			return ErrDuplicate
		}
		return fmt.Errorf("store: update category: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := s.GetCategory(ctx, c.ID); err != nil {
			return err
		}
	}
	return nil
}

// DeleteCategory removes a category and its entry and page links.
func (s *Store) DeleteCategory(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: delete category: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op
	for _, q := range []string{
		"DELETE FROM entry_categories WHERE category_id = ?",
		"DELETE FROM page_categories WHERE category_id = ?",
		"DELETE FROM categories WHERE id = ?",
	} {
		if _, err := tx.ExecContext(ctx, q, id); err != nil {
			return fmt.Errorf("store: delete category: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: delete category: %w", err)
	}
	return nil
}
