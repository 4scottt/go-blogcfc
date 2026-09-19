// Package migrate owns the schema: embedded SQL files applied in order and
// recorded in schema_migrations, plus the seed data (roles, settings) and
// the one-time admin user.
package migrate

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed sql/*.sql
var migrationFS embed.FS

type migration struct {
	version int
	name    string
	body    string
}

// load reads the embedded migrations, ordered by version.
func load() ([]migration, error) {
	entries, err := migrationFS.ReadDir("sql")
	if err != nil {
		return nil, fmt.Errorf("migrate: read embedded sql: %w", err)
	}
	var out []migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		num, _, ok := strings.Cut(e.Name(), "_")
		if !ok {
			return nil, fmt.Errorf("migrate: %s: name must be NNNN_description.sql", e.Name())
		}
		v, err := strconv.Atoi(num)
		if err != nil {
			return nil, fmt.Errorf("migrate: %s: %w", e.Name(), err)
		}
		body, err := migrationFS.ReadFile("sql/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("migrate: %s: %w", e.Name(), err)
		}
		out = append(out, migration{version: v, name: e.Name(), body: string(body)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

// Up applies every migration the database has not recorded yet. It is safe
// to run from an empty database and from any earlier version: the SQL is
// written to be re-runnable (CREATE TABLE IF NOT EXISTS and friends), because
// MariaDB cannot roll DDL back.
func Up(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version int NOT NULL,
		applied_at datetime NOT NULL,
		PRIMARY KEY (version)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`); err != nil {
		return fmt.Errorf("migrate: schema_migrations: %w", err)
	}

	applied, err := appliedVersions(ctx, db)
	if err != nil {
		return err
	}
	migrations, err := load()
	if err != nil {
		return err
	}
	for _, m := range migrations {
		if applied[m.version] {
			continue
		}
		slog.Info("applying migration", "version", m.version, "name", m.name)
		for _, stmt := range splitStatements(m.body) {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("migrate: %s: %w", m.name, err)
			}
		}
		// The version is recorded last: a crash halfway re-runs the file,
		// which its statements tolerate.
		if _, err := db.ExecContext(ctx,
			"INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?) ON DUPLICATE KEY UPDATE applied_at = VALUES(applied_at)",
			m.version, time.Now().UTC()); err != nil {
			return fmt.Errorf("migrate: record %s: %w", m.name, err)
		}
	}
	return nil
}

func appliedVersions(ctx context.Context, db *sql.DB) (map[int]bool, error) {
	rows, err := db.QueryContext(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("migrate: read schema_migrations: %w", err)
	}
	defer rows.Close()
	out := map[int]bool{}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("migrate: read schema_migrations: %w", err)
		}
		out[v] = true
	}
	return out, rows.Err()
}

// splitStatements cuts a migration file into statements on semicolons that
// end a line, ignoring `--` comment lines. The schema files hold DDL only,
// so nothing quoted contains a semicolon.
func splitStatements(body string) []string {
	var out []string
	var cur strings.Builder
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		cur.WriteString(line)
		cur.WriteString("\n")
		if strings.HasSuffix(trimmed, ";") {
			stmt := strings.TrimSpace(cur.String())
			out = append(out, strings.TrimSuffix(stmt, ";"))
			cur.Reset()
		}
	}
	if rest := strings.TrimSpace(cur.String()); rest != "" {
		out = append(out, rest)
	}
	return out
}
