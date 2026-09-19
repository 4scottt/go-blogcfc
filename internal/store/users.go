package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// User is an admin user. PasswordHash is a bcrypt hash; the plain password
// never reaches this package.
type User struct {
	Username     string
	PasswordHash string
	Name         string
	Roles        []Role
}

// HasRole reports whether the user holds the named role. Admin implies all.
func (u *User) HasRole(role string) bool {
	for _, r := range u.Roles {
		if strings.EqualFold(r.Role, role) || strings.EqualFold(r.Role, "Admin") {
			return true
		}
	}
	return false
}

// Role is one of the five seeded roles.
type Role struct {
	ID          int
	Role        string
	Description string
}

// ListRoles returns every role, by id.
func (s *Store) ListRoles(ctx context.Context) ([]Role, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, role, description FROM roles ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("store: list roles: %w", err)
	}
	defer rows.Close()
	var out []Role
	for rows.Next() {
		var r Role
		if err := rows.Scan(&r.ID, &r.Role, &r.Description); err != nil {
			return nil, fmt.Errorf("store: list roles: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetUser returns one user with their roles.
func (s *Store) GetUser(ctx context.Context, username string) (*User, error) {
	var u User
	err := s.db.QueryRowContext(ctx, "SELECT username, password_hash, name FROM users WHERE username = ?", username).
		Scan(&u.Username, &u.PasswordHash, &u.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get user: %w", err)
	}
	roles, err := s.userRoles(ctx, u.Username)
	if err != nil {
		return nil, err
	}
	u.Roles = roles
	return &u, nil
}

func (s *Store) userRoles(ctx context.Context, username string) ([]Role, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT r.id, r.role, r.description FROM user_roles ur
		JOIN roles r ON r.id = ur.role_id WHERE ur.username = ? ORDER BY r.id`, username)
	if err != nil {
		return nil, fmt.Errorf("store: user roles: %w", err)
	}
	defer rows.Close()
	var out []Role
	for rows.Next() {
		var r Role
		if err := rows.Scan(&r.ID, &r.Role, &r.Description); err != nil {
			return nil, fmt.Errorf("store: user roles: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListUsers returns every user with their roles, by username.
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT username, password_hash, name FROM users ORDER BY username")
	if err != nil {
		return nil, fmt.Errorf("store: list users: %w", err)
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.Username, &u.PasswordHash, &u.Name); err != nil {
			return nil, fmt.Errorf("store: list users: %w", err)
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list users: %w", err)
	}
	for i := range out {
		roles, err := s.userRoles(ctx, out[i].Username)
		if err != nil {
			return nil, err
		}
		out[i].Roles = roles
	}
	return out, nil
}

// CountUsers returns the number of users. Zero means the blog has never
// been seeded.
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count users: %w", err)
	}
	return n, nil
}

// CreateUser inserts a user and their roles.
func (s *Store) CreateUser(ctx context.Context, u *User, roleIDs []int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: create user: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op
	if _, err := tx.ExecContext(ctx, "INSERT INTO users (username, password_hash, name) VALUES (?,?,?)",
		u.Username, u.PasswordHash, u.Name); err != nil {
		if isDuplicate(err) {
			return ErrDuplicate
		}
		return fmt.Errorf("store: create user: %w", err)
	}
	if err := setUserRoles(ctx, tx, u.Username, roleIDs); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: create user: %w", err)
	}
	roles, err := s.userRoles(ctx, u.Username)
	if err != nil {
		return err
	}
	u.Roles = roles
	return nil
}

// UpdateUser writes a user's name and roles; the password hash is written
// only when PasswordHash is non-empty.
func (s *Store) UpdateUser(ctx context.Context, u *User, roleIDs []int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: update user: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op
	q, args := "UPDATE users SET name = ? WHERE username = ?", []any{u.Name, u.Username}
	if u.PasswordHash != "" {
		q, args = "UPDATE users SET name = ?, password_hash = ? WHERE username = ?", []any{u.Name, u.PasswordHash, u.Username}
	}
	res, err := tx.ExecContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("store: update user: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var exists string
		if err := tx.QueryRowContext(ctx, "SELECT username FROM users WHERE username = ?", u.Username).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
	}
	if err := setUserRoles(ctx, tx, u.Username, roleIDs); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: update user: %w", err)
	}
	roles, err := s.userRoles(ctx, u.Username)
	if err != nil {
		return err
	}
	u.Roles = roles
	return nil
}

func setUserRoles(ctx context.Context, tx *sql.Tx, username string, roleIDs []int) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM user_roles WHERE username = ?", username); err != nil {
		return fmt.Errorf("store: set user roles: %w", err)
	}
	seen := map[int]bool{}
	for _, id := range roleIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		if _, err := tx.ExecContext(ctx, "INSERT INTO user_roles (username, role_id) VALUES (?,?)", username, id); err != nil {
			return fmt.Errorf("store: set user roles: %w", err)
		}
	}
	return nil
}

// DeleteUser removes a user and their roles.
func (s *Store) DeleteUser(ctx context.Context, username string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: delete user: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op
	if _, err := tx.ExecContext(ctx, "DELETE FROM user_roles WHERE username = ?", username); err != nil {
		return fmt.Errorf("store: delete user: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM users WHERE username = ?", username); err != nil {
		return fmt.Errorf("store: delete user: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: delete user: %w", err)
	}
	return nil
}
