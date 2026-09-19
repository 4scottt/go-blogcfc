package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/4scottt/go-blogcfc/internal/auth"
	"github.com/4scottt/go-blogcfc/internal/store"
)

// SeedRole is one of BlogCFC's five roles. LegacyID is the id the as-is
// installer used, kept so an importer can map old rows.
type SeedRole struct {
	ID          int
	Role        string
	Description string
	LegacyID    string
}

// SeedRoles are the five roles from BlogCFC's MySQL installer
// (client/installer/mysql/script.txt), names and descriptions verbatim.
var SeedRoles = []SeedRole{
	{1, "AddCategory", "The ability to create a new category when editing a blog entry.", "7F183B27-FEDE-0D6F-E2E9C35DBC7BFF19"},
	{2, "ManageCategories", "The ability to manage blog categories.", "7F197F53-CFF7-18C8-53D0C85FCC2CA3F9"},
	{3, "Admin", "A special role for the admin. Allows all functionality.", "7F25A20B-EE6D-612D-24A7C0CEE6483EC2"},
	{4, "ManageUsers", "The ability to manage blog users.", "7F26DA6C-9F03-567F-ACFD34F62FB77199"},
	{5, "ReleaseEntries", "The ability to both release a new entry and edit any released entry.", "800CA7AA-0190-5329-D3C7753A59EA2589"},
}

// DefaultSettings are the seed values of the settings table (PLAN §10).
// Where BlogCFC's config/blog.ini.cfm has a value, it is that value; the
// keys §10 drops are not seeded, and `blogurl` is not stored at all (the
// accessor mirrors BLOG_BASE_URL).
func DefaultSettings() map[string]string {
	return map[string]string{
		// Blog Information
		"blogtitle":       "BlogCFC",
		"blogdescription": "",
		"blogkeywords":    "",
		"owneremail":      "info@your-domain.com",
		"failto":          "info@your-domain.com",

		// Content
		"commentsfrom":    "",
		"maxentries":      "10",
		"maxentriesadmin": "20",
		"timezone":        "UTC", // replaces BlogCFC's naive `offset`
		"pingurls":        "",
		"locale":          "en_US",

		// Content controls and security
		"ipblocklist":       "",
		"moderate":          "yes",
		"usecaptcha":        "yes",
		"usecfp":            "yes",
		"usetweetbacks":     "no",
		"trackbackspamlist": defaultTrackbackSpamList,
		"allowgravatars":    "yes",
		"filebrowse":        "yes",
		"imageroot":         "",

		// Podcasting
		"itunessubtitle": "",
		"itunessummary":  "",
		"ituneskeywords": "",
		"itunesauthor":   "",
		"itunesimage":    "",
		"itunesexplicit": "no",

		// Pods: the default set and order (PLAN §9 D10)
		"pods": `[{"name":"calendar","show":true,"order":1},` +
			`{"name":"subscribe","show":true,"order":2},` +
			`{"name":"recentcomments","show":true,"order":3},` +
			`{"name":"recent","show":true,"order":4},` +
			`{"name":"archives","show":true,"order":5}]`,
	}
}

// Seed inserts the roles and the default settings. It uses INSERT IGNORE
// throughout, so running it again leaves an operator's edits alone.
func Seed(ctx context.Context, db *sql.DB) error {
	for _, r := range SeedRoles {
		if _, err := db.ExecContext(ctx,
			"INSERT IGNORE INTO roles (id, role, description, legacy_id) VALUES (?,?,?,?)",
			r.ID, r.Role, r.Description, r.LegacyID); err != nil {
			return fmt.Errorf("migrate: seed role %s: %w", r.Role, err)
		}
	}
	for k, v := range DefaultSettings() {
		if _, err := db.ExecContext(ctx, "INSERT IGNORE INTO settings (`key`, value) VALUES (?,?)", k, v); err != nil {
			return fmt.Errorf("migrate: seed setting %s: %w", k, err)
		}
	}
	return nil
}

// SeedAdmin creates the `admin` user with every role, but only when the
// users table is empty: after that ADMIN_PASSWORD is ignored, as the
// runtime contract promises. The password is stored as a bcrypt hash.
func SeedAdmin(ctx context.Context, st *store.Store, password string) error {
	n, err := st.CountUsers(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		slog.Debug("admin seeding skipped, users exist", "users", n)
		return nil
	}
	if password == "" {
		return fmt.Errorf("migrate: seed admin: empty password")
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	roles, err := st.ListRoles(ctx)
	if err != nil {
		return err
	}
	ids := make([]int, 0, len(roles))
	for _, r := range roles {
		ids = append(ids, r.ID)
	}
	u := &store.User{Username: "admin", PasswordHash: hash, Name: "Admin"}
	if err := st.CreateUser(ctx, u, ids); err != nil {
		return fmt.Errorf("migrate: seed admin: %w", err)
	}
	slog.Info("admin user seeded", "roles", len(ids))
	return nil
}
