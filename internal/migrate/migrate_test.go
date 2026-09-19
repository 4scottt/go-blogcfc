package migrate_test

import (
	"context"
	"database/sql"
	"sort"
	"testing"

	"github.com/4scottt/go-blogcfc/internal/auth"
	"github.com/4scottt/go-blogcfc/internal/migrate"
	"github.com/4scottt/go-blogcfc/internal/store"
	"github.com/4scottt/go-blogcfc/internal/testdb"
)

var wantTables = []string{
	"categories", "comments", "enclosure_downloads", "entries", "entry_categories",
	"page_categories", "pages", "related_entries", "roles", "schema_migrations",
	"search_stats", "settings", "subscribers", "textblocks", "user_roles", "users",
}

// TestFP_O01_MigrationsIdempotentFromEmptyAndFromPrevious drops the schema,
// migrates from empty, migrates again, then forgets the last version's
// record and migrates once more. Every run must end with the same tables
// and the same five roles.
func TestFP_O01_MigrationsIdempotentFromEmptyAndFromPrevious(t *testing.T) {
	st := testdb.New(t)
	ctx := context.Background()
	db := st.DB()

	dropEverything(ctx, t, db)

	for i := range 2 {
		if err := migrate.Up(ctx, db); err != nil {
			t.Fatalf("Up (run %d from empty): %v", i+1, err)
		}
		if err := migrate.Seed(ctx, db); err != nil {
			t.Fatalf("Seed (run %d): %v", i+1, err)
		}
		assertSchema(ctx, t, st)
	}

	// From the previous version: forget the newest applied migration and
	// re-run, as an interrupted upgrade would.
	var last int
	if err := db.QueryRowContext(ctx, "SELECT MAX(version) FROM schema_migrations").Scan(&last); err != nil {
		t.Fatalf("read schema_migrations: %v", err)
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM schema_migrations WHERE version = ?", last); err != nil {
		t.Fatalf("drop the last migration record: %v", err)
	}
	if err := migrate.Up(ctx, db); err != nil {
		t.Fatalf("Up (from the previous version): %v", err)
	}
	if err := migrate.Seed(ctx, db); err != nil {
		t.Fatalf("Seed (from the previous version): %v", err)
	}
	assertSchema(ctx, t, st)

	var reapplied int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version = ?", last).Scan(&reapplied); err != nil {
		t.Fatalf("read schema_migrations: %v", err)
	}
	if reapplied != 1 {
		t.Fatalf("version %d recorded %d times, want 1", last, reapplied)
	}
}

// TestFP_O01_SeedRolesAndAdminOnlyWhenNoUsers checks the admin is seeded
// once, with every role, and that a later call with another password is a
// no-op (the runtime contract: ADMIN_PASSWORD is ignored after the first
// start).
func TestFP_O01_SeedRolesAndAdminOnlyWhenNoUsers(t *testing.T) {
	st := testdb.New(t)
	ctx := context.Background()

	n, err := st.CountUsers(ctx)
	if err != nil {
		t.Fatalf("CountUsers: %v", err)
	}
	if n != 0 {
		t.Fatalf("a fresh database has %d users, want 0", n)
	}

	if err := migrate.SeedAdmin(ctx, st, "first-password"); err != nil {
		t.Fatalf("SeedAdmin: %v", err)
	}
	u, err := st.GetUser(ctx, "admin")
	if err != nil {
		t.Fatalf("GetUser(admin): %v", err)
	}
	if len(u.Roles) != len(migrate.SeedRoles) {
		t.Fatalf("admin has %d roles, want %d", len(u.Roles), len(migrate.SeedRoles))
	}
	if !auth.CheckPassword(u.PasswordHash, "first-password") {
		t.Fatal("the stored hash does not match the seeded password")
	}
	if u.PasswordHash == "first-password" {
		t.Fatal("the password was stored in the clear")
	}

	// A second call must change nothing at all.
	if err := migrate.SeedAdmin(ctx, st, "second-password"); err != nil {
		t.Fatalf("SeedAdmin (second call): %v", err)
	}
	again, err := st.GetUser(ctx, "admin")
	if err != nil {
		t.Fatalf("GetUser(admin) after the second call: %v", err)
	}
	if again.PasswordHash != u.PasswordHash {
		t.Fatal("the second SeedAdmin call changed the password")
	}
	if auth.CheckPassword(again.PasswordHash, "second-password") {
		t.Fatal("the second password was accepted")
	}
	if n, err := st.CountUsers(ctx); err != nil || n != 1 {
		t.Fatalf("users = %d (err %v), want 1", n, err)
	}
}

// TestFP_O01_SettingsSeededOnceAndOperatorEditsSurvive checks re-seeding
// does not undo a settings change.
func TestFP_O01_SettingsSeededOnceAndOperatorEditsSurvive(t *testing.T) {
	st := testdb.New(t)
	ctx := context.Background()

	if err := st.SetSettings(ctx, map[string]string{"blogtitle": "An operator's title"}); err != nil {
		t.Fatalf("SetSettings: %v", err)
	}
	if err := migrate.Seed(ctx, st.DB()); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	all, err := st.AllSettings(ctx)
	if err != nil {
		t.Fatalf("AllSettings: %v", err)
	}
	if all["blogtitle"] != "An operator's title" {
		t.Fatalf("blogtitle = %q, want the operator's value", all["blogtitle"])
	}
	if _, ok := all["blogurl"]; ok {
		t.Fatal("blogurl must not be stored: it mirrors BLOG_BASE_URL")
	}
	for _, dropped := range []string{"dsn", "offset", "tableprefix", "installed", "users"} {
		if _, ok := all[dropped]; ok {
			t.Fatalf("dropped key %q was seeded", dropped)
		}
	}
}

func assertSchema(ctx context.Context, t *testing.T, st *store.Store) {
	t.Helper()
	got := tableNames(ctx, t, st.DB())
	if len(got) != len(wantTables) {
		t.Fatalf("tables = %v, want %v", got, wantTables)
	}
	for i := range got {
		if got[i] != wantTables[i] {
			t.Fatalf("tables = %v, want %v", got, wantTables)
		}
	}

	roles, err := st.ListRoles(ctx)
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	if len(roles) != 5 {
		t.Fatalf("roles = %d, want the five BlogCFC roles", len(roles))
	}
	for i, want := range migrate.SeedRoles {
		if roles[i].ID != want.ID || roles[i].Role != want.Role || roles[i].Description != want.Description {
			t.Fatalf("role %d = %+v, want %+v", i, roles[i], want)
		}
	}

	var settings int
	if err := st.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM settings").Scan(&settings); err != nil {
		t.Fatalf("count settings: %v", err)
	}
	if want := len(migrate.DefaultSettings()); settings != want {
		t.Fatalf("settings rows = %d, want %d", settings, want)
	}
}

func tableNames(ctx context.Context, t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.QueryContext(ctx,
		"SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE() AND table_type = 'BASE TABLE'")
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("list tables: %v", err)
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func dropEverything(ctx context.Context, t *testing.T, db *sql.DB) {
	t.Helper()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS = 0"); err != nil {
		t.Fatalf("disable foreign key checks: %v", err)
	}
	for _, tbl := range tableNames(ctx, t, db) {
		if _, err := conn.ExecContext(ctx, "DROP TABLE IF EXISTS `"+tbl+"`"); err != nil {
			t.Fatalf("drop %s: %v", tbl, err)
		}
	}
	if _, err := conn.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS = 1"); err != nil {
		t.Fatalf("enable foreign key checks: %v", err)
	}
}
