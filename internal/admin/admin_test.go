package admin_test

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/4scottt/go-blogcfc/internal/admin"
	"github.com/4scottt/go-blogcfc/internal/auth"
	"github.com/4scottt/go-blogcfc/internal/config"
	"github.com/4scottt/go-blogcfc/internal/store"
	"github.com/4scottt/go-blogcfc/internal/testdb"
)

const testPassword = "correct horse battery staple"

// harness is one admin on a real database, behind a client that keeps
// cookies but does not follow redirects, so every hop is assertable.
type harness struct {
	t      *testing.T
	module *admin.Module
	store  *store.Store
	server *httptest.Server
	client *http.Client
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	st := testdb.New(t)

	cfg := &config.Config{Port: 8080, BlogBaseURL: "http://127.0.0.1:8080", SessionSecret: "test-session-secret"}
	settings := config.NewSettings(st, cfg)
	if err := settings.Reload(context.Background()); err != nil {
		t.Fatalf("settings reload: %v", err)
	}
	sessions := auth.New(cfg.SessionSecret, false, st)
	m := admin.New(cfg, st, settings, sessions)

	mux := http.NewServeMux()
	m.Routes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	client := &http.Client{
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return &harness{t: t, module: m, store: st, server: srv, client: client}
}

// user creates a user with the named seeded roles.
func (h *harness) user(username string, roleNames ...string) *store.User {
	h.t.Helper()
	ctx := context.Background()
	all, err := h.store.ListRoles(ctx)
	if err != nil {
		h.t.Fatalf("ListRoles: %v", err)
	}
	var ids []int
	for _, want := range roleNames {
		found := false
		for _, r := range all {
			if strings.EqualFold(r.Role, want) {
				ids, found = append(ids, r.ID), true
			}
		}
		if !found {
			h.t.Fatalf("role %q is not seeded", want)
		}
	}
	hash, err := auth.HashPassword(testPassword)
	if err != nil {
		h.t.Fatalf("HashPassword: %v", err)
	}
	u := &store.User{Username: username, PasswordHash: hash, Name: username}
	if err := h.store.CreateUser(ctx, u, ids); err != nil {
		h.t.Fatalf("CreateUser(%s): %v", username, err)
	}
	return u
}

func (h *harness) get(path string) (*http.Response, string) {
	h.t.Helper()
	resp, err := h.client.Get(h.server.URL + path)
	if err != nil {
		h.t.Fatalf("GET %s: %v", path, err)
	}
	return resp, readBody(h.t, resp)
}

func (h *harness) postLogin(form url.Values) (*http.Response, string) {
	h.t.Helper()
	resp, err := h.client.PostForm(h.server.URL+"/admin/login", form)
	if err != nil {
		h.t.Fatalf("POST /admin/login: %v", err)
	}
	return resp, readBody(h.t, resp)
}

// login signs in and fails the test if it does not land on the dashboard.
func (h *harness) login(username string) {
	h.t.Helper()
	resp, _ := h.postLogin(url.Values{"username": {username}, "password": {testPassword}, "login": {"Login"}})
	if resp.StatusCode != http.StatusFound {
		h.t.Fatalf("login as %s: status %d, want 302", username, resp.StatusCode)
	}
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

// TestFP_A01_LoginFailureDelaysAndSucceedsThenLogout covers PLAN §9 A01:
// the 500 ms delay on a bad password, the redirect on a good one, the
// dashboard behind the cookie, and logout clearing it.
func TestFP_A01_LoginFailureDelaysAndSucceedsThenLogout(t *testing.T) {
	h := newHarness(t)
	h.user("admin", "Admin")

	// The form itself is reachable without a session and carries the
	// controls the Pilot drives by name.
	_, form := h.get("/admin/login")
	for _, want := range []string{`name="username"`, `name="password"`, `value="Login"`} {
		if !strings.Contains(form, want) {
			t.Errorf("the login form has no %s", want)
		}
	}

	start := time.Now()
	resp, body := h.postLogin(url.Values{"username": {"admin"}, "password": {"wrong"}, "login": {"Login"}})
	elapsed := time.Since(start)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bad password: status %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "Invalid login") {
		t.Errorf("bad password: body has no %q", "Invalid login")
	}
	if elapsed < 500*time.Millisecond {
		t.Errorf("bad password answered in %v, want at least 500ms (brute-force delay)", elapsed)
	}
	if len(h.client.Jar.Cookies(mustParse(t, h.server.URL))) != 0 {
		t.Error("a failed login set a cookie")
	}

	resp, _ = h.postLogin(url.Values{"username": {"admin"}, "password": {testPassword}, "login": {"Login"}})
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("good password: status %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/admin/" {
		t.Fatalf("good password: Location = %q, want /admin/", loc)
	}
	if len(h.client.Jar.Cookies(mustParse(t, h.server.URL))) == 0 {
		t.Fatal("a successful login set no cookie")
	}

	resp, body = h.get("/admin/")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("dashboard: status %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "Welcome to BlogCFC Administrator") {
		t.Errorf("the dashboard heading is not %q", "Welcome to BlogCFC Administrator")
	}

	resp, _ = h.get("/admin/logout")
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/admin/login" {
		t.Fatalf("logout: status %d to %q, want 302 to /admin/login", resp.StatusCode, resp.Header.Get("Location"))
	}
	if cookies := h.client.Jar.Cookies(mustParse(t, h.server.URL)); len(cookies) != 0 {
		t.Errorf("logout left %d cookies in the jar", len(cookies))
	}

	resp, _ = h.get("/admin/")
	if resp.StatusCode != http.StatusFound || !strings.HasPrefix(resp.Header.Get("Location"), "/admin/login") {
		t.Fatalf("after logout: status %d to %q, want 302 to the login", resp.StatusCode, resp.Header.Get("Location"))
	}
}

// TestFP_A01_AllAdminRoutesGated: the gate covers the paths that exist,
// the ones a later package will add, and the ones that never will.
func TestFP_A01_AllAdminRoutesGated(t *testing.T) {
	h := newHarness(t)
	h.user("admin", "Admin")

	for _, path := range []string{"/admin/", "/admin/entries", "/admin/categories", "/admin/settings", "/admin/nothing"} {
		resp, _ := h.get(path)
		if resp.StatusCode != http.StatusFound {
			t.Errorf("GET %s signed out: status %d, want 302", path, resp.StatusCode)
			continue
		}
		loc := resp.Header.Get("Location")
		if !strings.HasPrefix(loc, "/admin/login?return=") {
			t.Errorf("GET %s signed out: Location = %q, want the login with a return", path, loc)
			continue
		}
		ret, err := url.Parse(loc)
		if err != nil {
			t.Fatalf("parse %q: %v", loc, err)
		}
		if got := ret.Query().Get("return"); got != path {
			t.Errorf("GET %s: return = %q, want %q", path, got, path)
		}
	}

	// Signed in, an unknown admin page is a 404 rather than a redirect.
	h.login("admin")
	resp, body := h.get("/admin/nothing")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /admin/nothing signed in: status %d, want 404", resp.StatusCode)
	}
	if !strings.Contains(body, "Not found") {
		t.Error("the 404 page has no heading")
	}
}

// TestFP_A02_DashboardVersionTopEntriesAndReinitBanner covers PLAN §9
// A02: the version line, the seven-day top-entries table in view order,
// and the ?reinit=1 banner with its hook.
func TestFP_A02_DashboardVersionTopEntriesAndReinitBanner(t *testing.T) {
	h := newHarness(t)
	h.user("admin", "Admin")
	ctx := context.Background()
	now := time.Now().UTC()

	entries := []*store.Entry{
		{Title: "Quiet recent entry", Alias: "quiet", Body: "b", Posted: now.Add(-24 * time.Hour), Username: "admin", Released: true, Views: 5},
		{Title: "Popular recent entry", Alias: "popular", Body: "b", Posted: now.Add(-48 * time.Hour), Username: "admin", Released: true, Views: 90},
		{Title: "Popular old entry", Alias: "old", Body: "b", Posted: now.Add(-10 * 24 * time.Hour), Username: "admin", Released: true, Views: 900},
	}
	for _, e := range entries {
		if err := h.store.CreateEntry(ctx, e); err != nil {
			t.Fatalf("CreateEntry(%s): %v", e.Title, err)
		}
	}

	reinits := 0
	h.module.Reinit = func() { reinits++ }

	h.login("admin")
	resp, body := h.get("/admin/")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("dashboard: status %d, want 200", resp.StatusCode)
	}
	if want := "go-blogcfc " + admin.Version; !strings.Contains(body, want) {
		t.Errorf("the dashboard does not say %q", want)
	}
	if !strings.Contains(body, "Top entries, last 7 days") {
		t.Error("the dashboard has no top-entries table")
	}
	if strings.Contains(body, "Popular old entry") {
		t.Error("an entry older than seven days is in the top-entries table")
	}
	popular, quiet := strings.Index(body, "Popular recent entry"), strings.Index(body, "Quiet recent entry")
	if popular < 0 || quiet < 0 {
		t.Fatalf("the recent entries are missing (popular %d, quiet %d)", popular, quiet)
	}
	if popular > quiet {
		t.Error("the top-entries table is not ordered by views, highest first")
	}
	if strings.Contains(body, "Caches reinitialized") {
		t.Error("the cache banner shows without ?reinit")
	}
	if reinits != 0 {
		t.Errorf("the reinit hook ran %d times on a plain dashboard", reinits)
	}

	_, body = h.get("/admin/?reinit=1")
	if !strings.Contains(body, "Caches reinitialized") {
		t.Error("?reinit=1 shows no banner")
	}
	if reinits != 1 {
		t.Errorf("the reinit hook ran %d times, want 1", reinits)
	}
}

// TestFP_A19_MenuOmitsScreensTheUserMayNotSee: the left menu is built
// from the user's roles (PLAN §12), so a user without ManageUsers is
// never shown Users.
func TestFP_A19_MenuOmitsScreensTheUserMayNotSee(t *testing.T) {
	h := newHarness(t)
	h.user("admin", "Admin")
	h.user("writer", "AddCategory")

	h.login("writer")
	_, body := h.get("/admin/")
	if !strings.Contains(body, `id="menu"`) {
		t.Fatal("the admin frame has no #menu")
	}
	if !strings.Contains(body, "/admin/entries") {
		t.Error("the menu omits Entries, which needs only a login")
	}
	for _, gone := range []string{"/admin/users", "/admin/categories", "/admin/pages"} {
		if strings.Contains(body, gone) {
			t.Errorf("the menu shows %s to a user without the role", gone)
		}
	}
	if strings.Contains(body, ">Users<") {
		t.Error("the Users group is shown although all its links are hidden")
	}

	h.get("/admin/logout")
	h.login("admin")
	_, body = h.get("/admin/")
	for _, want := range []string{"/admin/users", "/admin/categories", "/admin/pages", "/admin/settings", "/admin/password", "/admin/logout"} {
		if !strings.Contains(body, want) {
			t.Errorf("the admin's menu omits %s", want)
		}
	}
	if !strings.Contains(body, `id="blogTitle"`) || !strings.Contains(body, `id="header"`) || !strings.Contains(body, `id="content"`) {
		t.Error("the admin frame is missing one of #blogTitle, #header, #content")
	}
}

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u
}
