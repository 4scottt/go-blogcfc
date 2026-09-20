package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Page is a static page (BlogCFC's page.cfm): a title, a permalink alias,
// a body and whether the site layout wraps it. CategoryIDs is filled by
// the getters and written by SetPageCategories.
type Page struct {
	ID          string
	Title       string
	Alias       string
	Body        string
	ShowLayout  bool
	CategoryIDs []string
}

// ListPages returns every page by title, as BlogCFC's getPages does.
func (s *Store) ListPages(ctx context.Context) ([]Page, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, title, alias, body, showlayout FROM pages ORDER BY title ASC, id ASC")
	if err != nil {
		return nil, fmt.Errorf("store: list pages: %w", err)
	}
	defer rows.Close()
	var out []Page
	for rows.Next() {
		var p Page
		if err := rows.Scan(&p.ID, &p.Title, &p.Alias, &p.Body, &p.ShowLayout); err != nil {
			return nil, fmt.Errorf("store: list pages: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list pages: %w", err)
	}
	if err := s.attachPageCategories(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

// attachPageCategories fills CategoryIDs on every page in one query.
func (s *Store) attachPageCategories(ctx context.Context, pages []Page) error {
	if len(pages) == 0 {
		return nil
	}
	idx := make(map[string]*Page, len(pages))
	args := make([]any, 0, len(pages))
	for i := range pages {
		idx[pages[i].ID] = &pages[i]
		args = append(args, pages[i].ID)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT pc.page_id, c.id FROM page_categories pc
		JOIN categories c ON c.id = pc.category_id
		WHERE pc.page_id IN (`+placeholders(len(args))+`) ORDER BY c.name`, args...)
	if err != nil {
		return fmt.Errorf("store: page categories: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var pageID, catID string
		if err := rows.Scan(&pageID, &catID); err != nil {
			return fmt.Errorf("store: page categories: %w", err)
		}
		if p := idx[pageID]; p != nil {
			p.CategoryIDs = append(p.CategoryIDs, catID)
		}
	}
	return rows.Err()
}

// GetPage returns one page by id.
func (s *Store) GetPage(ctx context.Context, id string) (*Page, error) {
	return s.getPageBy(ctx, "id", id)
}

// GetPageByAlias returns the page behind /page/{alias}.
func (s *Store) GetPageByAlias(ctx context.Context, alias string) (*Page, error) {
	return s.getPageBy(ctx, "alias", alias)
}

func (s *Store) getPageBy(ctx context.Context, col, val string) (*Page, error) {
	var p Page
	err := s.db.QueryRowContext(ctx,
		"SELECT id, title, alias, body, showlayout FROM pages WHERE "+col+" = ? LIMIT 1", val).
		Scan(&p.ID, &p.Title, &p.Alias, &p.Body, &p.ShowLayout)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get page: %w", err)
	}
	one := []Page{p}
	if err := s.attachPageCategories(ctx, one); err != nil {
		return nil, err
	}
	return &one[0], nil
}

// CreatePage inserts a page, filling ID when it is empty. A clash on the
// alias is ErrDuplicate. Categories are a separate write.
func (s *Store) CreatePage(ctx context.Context, p *Page) error {
	if p.ID == "" {
		id, err := newID()
		if err != nil {
			return err
		}
		p.ID = id
	}
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO pages (id, title, alias, body, showlayout) VALUES (?,?,?,?,?)",
		p.ID, p.Title, p.Alias, p.Body, p.ShowLayout)
	if err != nil {
		if isDuplicate(err) {
			return ErrDuplicate
		}
		return fmt.Errorf("store: create page: %w", err)
	}
	return nil
}

// UpdatePage writes a page's columns.
func (s *Store) UpdatePage(ctx context.Context, p *Page) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE pages SET title = ?, alias = ?, body = ?, showlayout = ? WHERE id = ?",
		p.Title, p.Alias, p.Body, p.ShowLayout, p.ID)
	if err != nil {
		if isDuplicate(err) {
			return ErrDuplicate
		}
		return fmt.Errorf("store: update page: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := s.GetPage(ctx, p.ID); err != nil {
			return err
		}
	}
	return nil
}

// DeletePage removes a page and its category links.
func (s *Store) DeletePage(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: delete page: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op
	for _, q := range []string{
		"DELETE FROM page_categories WHERE page_id = ?",
		"DELETE FROM pages WHERE id = ?",
	} {
		if _, err := tx.ExecContext(ctx, q, id); err != nil {
			return fmt.Errorf("store: delete page: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: delete page: %w", err)
	}
	return nil
}

// SetPageCategories replaces a page's category set.
func (s *Store) SetPageCategories(ctx context.Context, pageID string, catIDs []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: set page categories: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op
	if _, err := tx.ExecContext(ctx, "DELETE FROM page_categories WHERE page_id = ?", pageID); err != nil {
		return fmt.Errorf("store: set page categories: %w", err)
	}
	seen := map[string]bool{}
	for _, id := range catIDs {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO page_categories (page_id, category_id) VALUES (?, ?)", pageID, id); err != nil {
			return fmt.Errorf("store: set page categories: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: set page categories: %w", err)
	}
	return nil
}
