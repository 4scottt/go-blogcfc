// Package store is the database layer: database/sql over MariaDB, one file
// per aggregate. No SQL lives outside this package.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"
)

// Store holds the pool. It is safe for concurrent use.
type Store struct {
	db *sql.DB
}

// Open prepares a pool for the DSN. It does not connect; call Ping for that.
func Open(dsn string) (*Store, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	db.SetMaxOpenConns(16)
	db.SetMaxIdleConns(8)
	db.SetConnMaxLifetime(30 * time.Minute)
	return &Store{db: db}, nil
}

// DB exposes the pool for the migrator and for tests. Handlers use the
// methods on Store instead.
func (s *Store) DB() *sql.DB { return s.db }

// Ping checks the database is reachable.
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

// Close releases the pool.
func (s *Store) Close() error { return s.db.Close() }

// newID returns a random RFC 4122 version 4 UUID in the char(36) form the
// schema uses. crypto/rand keeps the dependency list short.
func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("store: uuid: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// NewID returns a fresh UUID, for callers that need an id before the row.
func NewID() (string, error) { return newID() }

// isDuplicate reports whether err is MariaDB's duplicate key error.
func isDuplicate(err error) bool {
	var me *mysql.MySQLError
	if errors.As(err, &me) {
		return me.Number == 1062
	}
	return false
}

// utcOrZero normalises a time for storage: UTC, seconds precision.
func utcOrZero(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	return t.UTC().Truncate(time.Second)
}
