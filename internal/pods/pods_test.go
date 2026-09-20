package pods

import (
	"context"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/4scottt/go-blogcfc/internal/config"
	"github.com/4scottt/go-blogcfc/internal/mail"
	"github.com/4scottt/go-blogcfc/internal/store"
	"github.com/4scottt/go-blogcfc/internal/testdb"
)

// updateGolden rewrites the testdata files instead of comparing them:
// `go test ./internal/pods/ -update`.
var updateGolden = flag.Bool("update", false, "update the golden files in testdata")

// testBase is deliberately not the httptest host: every URL a pod draws
// comes from BLOG_BASE_URL (PLAN §6, FP O05).
const testBase = "http://blog.example"

// testSite is the module on a clean database, in a zone that is not UTC.
type testSite struct {
	t        *testing.T
	store    *store.Store
	settings *config.Settings
	mails    *mail.Recorder
	mod      *Module
	handler  http.Handler
	loc      *time.Location
}

func newTestSite(t *testing.T) *testSite {
	t.Helper()
	st := testdb.New(t)
	cfg := &config.Config{BlogBaseURL: testBase, Port: 8080}
	settings := config.NewSettings(st, cfg)
	ctx := context.Background()
	if err := settings.Reload(ctx); err != nil {
		t.Fatalf("settings reload: %v", err)
	}
	// A zone whose offset changes with the seasons: the calendar's days,
	// the monthly archives' months and today all follow it (PLAN §11).
	if err := settings.Set(ctx, map[string]string{
		"timezone":   "America/Los_Angeles",
		"blogtitle":  "Test Blog",
		"owneremail": "owner@example.com",
		"locale":     "en_US",
	}); err != nil {
		t.Fatalf("set settings: %v", err)
	}
	if settings.Timezone() == time.UTC {
		t.Skip("the America/Los_Angeles zone is not available here")
	}
	rec := &mail.Recorder{}
	mod := New(cfg, st, settings, rec)
	mux := http.NewServeMux()
	mod.Routes(mux)
	return &testSite{t: t, store: st, settings: settings, mails: rec,
		mod: mod, handler: mux, loc: settings.Timezone()}
}

// only makes one pod the whole sidebar, so a golden holds that pod alone.
func (s *testSite) only(name string) {
	s.t.Helper()
	if err := s.settings.SetPods(context.Background(),
		[]config.Pod{{Name: name, Show: true, Order: 1}}); err != nil {
		s.t.Fatalf("set pods: %v", err)
	}
}

// sidebar renders the column for a request on that path.
func (s *testSite) sidebar(path string) string {
	s.t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	return string(s.mod.Sidebar(r))
}

func (s *testSite) post(path string, form map[string]string) *httptest.ResponseRecorder {
	s.t.Helper()
	values := make([]string, 0, len(form))
	for k, v := range form {
		values = append(values, k+"="+v)
	}
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(strings.Join(values, "&")))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, r)
	return rec
}

func (s *testSite) entry(e store.Entry) store.Entry {
	s.t.Helper()
	if e.Username == "" {
		e.Username = "admin"
	}
	if e.Body == "" {
		e.Body = "<p>" + e.Title + " body.</p>"
	}
	if err := s.store.CreateEntry(context.Background(), &e); err != nil {
		s.t.Fatalf("create entry %q: %v", e.Title, err)
	}
	return e
}

func (s *testSite) category(name, alias string) store.Category {
	s.t.Helper()
	c := store.Category{Name: name, Alias: alias}
	if err := s.store.CreateCategory(context.Background(), &c); err != nil {
		s.t.Fatalf("create category %q: %v", name, err)
	}
	return c
}

// categoryWithEntries creates a category and n live entries in it.
func (s *testSite) categoryWithEntries(name, alias string, n int) store.Category {
	s.t.Helper()
	c := s.category(name, alias)
	for i := 0; i < n; i++ {
		e := s.entry(store.Entry{
			Title:    name + " entry",
			Released: true,
			Posted:   time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Hour),
		})
		if err := s.store.SetEntryCategories(context.Background(), e.ID, []string{c.ID}); err != nil {
			s.t.Fatalf("set categories: %v", err)
		}
	}
	return c
}

func (s *testSite) comment(c store.Comment) store.Comment {
	s.t.Helper()
	c.Moderated = true
	if err := s.store.CreateComment(context.Background(), &c); err != nil {
		s.t.Fatalf("create comment: %v", err)
	}
	return c
}

func (s *testSite) page(p store.Page) store.Page {
	s.t.Helper()
	if err := s.store.CreatePage(context.Background(), &p); err != nil {
		s.t.Fatalf("create page %q: %v", p.Title, err)
	}
	return p
}

// checkGolden compares a rendering with testdata/<name>, or rewrites it
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
		t.Fatalf("golden %s: %v (run `go test ./internal/pods/ -update`)", name, err)
	}
	if strings.TrimRight(string(want), "\n") != strings.TrimRight(got, "\n") {
		t.Errorf("golden %s does not match.\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

var titlePattern = regexp.MustCompile(`<h4>([^<]*)</h4>`)

// titles are the pod headings in the order they were rendered.
func titles(html string) []string {
	var out []string
	for _, m := range titlePattern.FindAllStringSubmatch(html, -1) {
		out = append(out, m[1])
	}
	return out
}

func mustContain(t *testing.T, html string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(html, w) {
			t.Errorf("rendering does not contain %q:\n%s", w, html)
		}
	}
}

func mustNotContain(t *testing.T, html string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if strings.Contains(html, w) {
			t.Errorf("rendering should not contain %q:\n%s", w, html)
		}
	}
}

// newRequest is a GET for a path, without a site around it.
func newRequest(path string) *http.Request {
	return httptest.NewRequest(http.MethodGet, path, nil)
}

// itoa keeps the test's string building readable.
func itoa(n int) string { return strconv.Itoa(n) }

// newContext is the background context the fixtures write with.
func newContext() context.Context { return context.Background() }
