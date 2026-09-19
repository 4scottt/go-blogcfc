package web

import (
	"context"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/4scottt/go-blogcfc/internal/config"
	"github.com/4scottt/go-blogcfc/internal/store"
	"github.com/4scottt/go-blogcfc/internal/testdb"
)

// updateGolden rewrites the testdata files instead of comparing them:
// `go test ./internal/web/ -update`.
var updateGolden = flag.Bool("update", false, "update the golden files in testdata")

// testBase is deliberately not the httptest host: every link must come
// from BLOG_BASE_URL, never from the request (PLAN §6, FP O05).
const testBase = "http://blog.example"

// fakeIdentity is the auth package's stand-in: it answers with whatever
// user the test put in it, or nobody.
type fakeIdentity struct{ user *store.User }

func (f fakeIdentity) Current(*http.Request) *store.User { return f.user }

// testSite is one wired-up public site on a clean database.
type testSite struct {
	t        *testing.T
	store    *store.Store
	settings *config.Settings
	handler  http.Handler
}

// newTestSite builds the module against a real MariaDB (testdb) with the
// seeded settings, and a mux holding only the public routes.
func newTestSite(t *testing.T, identity Identity) *testSite {
	t.Helper()
	st := testdb.New(t)
	cfg := &config.Config{BlogBaseURL: testBase, Port: 8080}
	settings := config.NewSettings(st, cfg)
	if err := settings.Reload(context.Background()); err != nil {
		t.Fatalf("settings reload: %v", err)
	}
	mux := http.NewServeMux()
	New(cfg, st, settings, identity).Routes(mux)
	return &testSite{t: t, store: st, settings: settings, handler: mux}
}

// get runs one GET through the mux.
func (s *testSite) get(path string) *httptest.ResponseRecorder {
	s.t.Helper()
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// getOK runs one GET and fails unless it answers 200.
func (s *testSite) getOK(path string) string {
	s.t.Helper()
	rec := s.get(path)
	if rec.Code != http.StatusOK {
		s.t.Fatalf("GET %s = %d, want 200", path, rec.Code)
	}
	return rec.Body.String()
}

// setSetting writes one setting and reloads the cache.
func (s *testSite) setSetting(key, value string) {
	s.t.Helper()
	if err := s.settings.Set(context.Background(), map[string]string{key: value}); err != nil {
		s.t.Fatalf("set %s: %v", key, err)
	}
}

// entry inserts one entry through the store, never with raw SQL.
func (s *testSite) entry(e store.Entry) store.Entry {
	s.t.Helper()
	if e.Body == "" {
		e.Body = "<p>" + e.Title + " body.</p>"
	}
	if err := s.store.CreateEntry(context.Background(), &e); err != nil {
		s.t.Fatalf("create entry %q: %v", e.Title, err)
	}
	return e
}

// category inserts one category.
func (s *testSite) category(id, name, alias string) store.Category {
	s.t.Helper()
	c := store.Category{ID: id, Name: name, Alias: alias}
	if err := s.store.CreateCategory(context.Background(), &c); err != nil {
		s.t.Fatalf("create category %q: %v", name, err)
	}
	return c
}

// categorise links an entry to categories.
func (s *testSite) categorise(entryID string, catIDs ...string) {
	s.t.Helper()
	if err := s.store.SetEntryCategories(context.Background(), entryID, catIDs); err != nil {
		s.t.Fatalf("set entry categories: %v", err)
	}
}

// user inserts one author.
func (s *testSite) user(username, name string) *store.User {
	s.t.Helper()
	u := &store.User{Username: username, Name: name, PasswordHash: "$2a$10$notarealhash"}
	if err := s.store.CreateUser(context.Background(), u, nil); err != nil {
		s.t.Fatalf("create user %q: %v", username, err)
	}
	return u
}

// utc is a fixture time in UTC, seconds precision.
func utc(y int, mo time.Month, d, h, mi int) time.Time {
	return time.Date(y, mo, d, h, mi, 0, 0, time.UTC)
}

var (
	// html/template drops comments, so an entry block is bounded by its
	// own closing tag: every tag inside it is indented, and only the
	// post's own </div> sits at the start of a line.
	postBlock  = regexp.MustCompile(`(?s)<div class="post">.*?\n</div>`)
	pagerBlock = regexp.MustCompile(`(?s)<p class="pager">.*?</p>`)
	titleLinks = regexp.MustCompile(`<h3 class="post-title"><a href="[^"]*">([^<]*)</a></h3>`)
)

// postTitles returns the entry titles a page lists, in order.
func postTitles(body string) []string {
	var out []string
	for _, m := range titleLinks.FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

// countPosts counts the rendered entry blocks.
func countPosts(body string) int { return len(postBlock.FindAllString(body, -1)) }

// checkGolden compares one fragment with testdata/<name>, or rewrites it
// under -update.
func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *updateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("golden: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden %s: %v (run `go test ./internal/web/ -update`)", name, err)
	}
	if strings.TrimRight(string(want), "\n") != strings.TrimRight(got, "\n") {
		t.Errorf("golden %s does not match.\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

// TestLayoutSkeletonMatchesGolden holds the DOM skeleton of PLAN §12: the
// ids and classes the walk, the Pilot and a screenshot comparison all
// depend on, plus a golden of the whole (empty) home page.
func TestLayoutSkeletonMatchesGolden(t *testing.T) {
	s := newTestSite(t, nil)
	body := s.getOK("/")

	for _, want := range []string{
		`<div id="page" class="with-sidebar">`,
		`<div id="header-wrap">`,
		`<div id="header" class="block-content">`,
		`<div id="pagetitle">`,
		`<h1 class="logo"><a href="` + testBase + `/">`,
		`<div class="search-block">`,
		`<form method="get" id="searchform" action="` + testBase + `/search">`,
		`<input type="text" name="search" id="searchbox"`,
		`class="go"`,
		`<div id="nav-wrap1">`,
		`<div id="nav-wrap2">`,
		`<ul id="nav">`,
		`<div id="main-wrap1">`,
		`<div id="main-wrap2">`,
		`<div id="main" class="block-content">`,
		`<div class="col1">`,
		`<div id="main-content">`,
		`<div class="col2">`,
		`<ul id="sidebar">`,
		`<div id="footer">`,
		`<div class="copyright">`,
		`go-blogcfc ` + Version + `, a rewrite of BlogCFC by Raymond Camden |`,
		`<link rel="alternate" type="application/rss+xml" title="RSS" href="` + testBase + `/rss" />`,
		`<a href="` + testBase + `/contact">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("home page is missing %q", want)
		}
	}
	// The sidebar stays empty until the pods land in M2.
	if !strings.Contains(body, `<ul id="sidebar"></ul>`) {
		t.Error("sidebar should be empty in M1")
	}
	// Nothing on the page may point at the request's host.
	if strings.Contains(body, "example.com") {
		t.Error("a link was built from the request Host, not BLOG_BASE_URL")
	}
	checkGolden(t, "layout_home_empty.html", body)
}
