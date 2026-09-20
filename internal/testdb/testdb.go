// Package testdb gives handler and store tests a real MariaDB: migrated,
// emptied and re-seeded before every test. It is the only place tests
// touch the database directly.
package testdb

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/4scottt/go-blogcfc/internal/migrate"
	"github.com/4scottt/go-blogcfc/internal/store"
)

// skipMessage tells whoever runs the suite how to get a database.
const skipMessage = "TEST_DSN is not set: start MariaDB with " +
	"`docker compose -f deploy/compose.yaml up -d db` and run scripts/test.sh " +
	"(TEST_DSN='goblogcfc:goblogcfc@tcp(127.0.0.1:3307)/goblogcfc_test?parseTime=true&loc=UTC&multiStatements=true')"

// New returns a store on a clean, migrated, seeded database. It skips the
// test when TEST_DSN is unset, so `go test ./...` is green without Docker.
func New(t *testing.T) *store.Store {
	t.Helper()
	dsn := os.Getenv("TEST_DSN")
	if dsn == "" {
		t.Skip(skipMessage)
	}

	st, err := store.Open(dsn)
	if err != nil {
		t.Fatalf("testdb: open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := st.Ping(ctx); err != nil {
		t.Fatalf("testdb: ping %s: %v", dsn, err)
	}
	// One database serves every package, so tests take a named lock for
	// their duration: `go test ./...` runs packages in parallel and two
	// truncations at once would make a mess.
	lock(ctx, t, st)
	if err := migrate.Up(ctx, st.DB()); err != nil {
		t.Fatalf("testdb: migrate: %v", err)
	}
	if err := Truncate(ctx, st); err != nil {
		t.Fatalf("testdb: truncate: %v", err)
	}
	if err := migrate.Seed(ctx, st.DB()); err != nil {
		t.Fatalf("testdb: seed: %v", err)
	}
	return st
}

// lockName is the advisory lock every test holds while it owns the
// database.
const lockName = "go-blogcfc-testdb"

func lock(ctx context.Context, t *testing.T, st *store.Store) {
	t.Helper()
	conn, err := st.DB().Conn(context.Background())
	if err != nil {
		t.Fatalf("testdb: lock connection: %v", err)
	}
	var got sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 120)", lockName).Scan(&got); err != nil {
		conn.Close()
		t.Fatalf("testdb: get lock: %v", err)
	}
	if !got.Valid || got.Int64 != 1 {
		conn.Close()
		t.Fatalf("testdb: another test still holds %s after 120s", lockName)
	}
	t.Cleanup(func() {
		_, _ = conn.ExecContext(context.Background(), "SELECT RELEASE_LOCK(?)", lockName)
		conn.Close()
	})
}

// Truncate empties every table but schema_migrations, which records the
// migrations already applied to this database. Everything runs on one
// connection, because FOREIGN_KEY_CHECKS is a session variable.
func Truncate(ctx context.Context, st *store.Store) error {
	conn, err := st.DB().Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	rows, err := conn.QueryContext(ctx,
		"SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE() AND table_type = 'BASE TABLE'")
	if err != nil {
		return err
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		if name != "schema_migrations" {
			tables = append(tables, name)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	if _, err := conn.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS = 0"); err != nil {
		return err
	}
	defer func() { _, _ = conn.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS = 1") }()
	for _, tbl := range tables {
		if _, err := conn.ExecContext(ctx, "TRUNCATE TABLE `"+tbl+"`"); err != nil {
			return err
		}
	}
	return nil
}
