package feeds

import (
	"context"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/4scottt/go-blogcfc/internal/config"
	"github.com/4scottt/go-blogcfc/internal/store"
	"github.com/4scottt/go-blogcfc/internal/testdb"
)

// updateGolden rewrites the testdata files instead of comparing them:
// `go test ./internal/feeds/ -update`.
var updateGolden = flag.Bool("update", false, "update the golden files in testdata")

// testBase is deliberately not the httptest host: every URL in the
// sitemap comes from BLOG_BASE_URL (PLAN §6, FP O05).
const testBase = "http://blog.example"

// testSite is the module on a clean database with a fixed blog zone.
type testSite struct {
	t        *testing.T
	store    *store.Store
	settings *config.Settings
	handler  http.Handler
}

func newTestSite(t *testing.T) *testSite {
	t.Helper()
	st := testdb.New(t)
	cfg := &config.Config{BlogBaseURL: testBase, Port: 8080}
	settings := config.NewSettings(st, cfg)
	if err := settings.Reload(context.Background()); err != nil {
		t.Fatalf("settings reload: %v", err)
	}
	// A zone that is not UTC, and whose offset changes with the seasons:
	// the stamps and the permalink dates must follow it.
	if err := settings.Set(context.Background(), map[string]string{"timezone": "America/Los_Angeles"}); err != nil {
		t.Fatalf("set timezone: %v", err)
	}
	if settings.Timezone() == time.UTC {
		t.Skip("the America/Los_Angeles zone is not available here")
	}
	mux := http.NewServeMux()
	NewSitemap(cfg, st, settings).Routes(mux)
	return &testSite{t: t, store: st, settings: settings, handler: mux}
}

func (s *testSite) get(path string) *httptest.ResponseRecorder {
	s.t.Helper()
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

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

func (s *testSite) page(p store.Page) store.Page {
	s.t.Helper()
	if err := s.store.CreatePage(context.Background(), &p); err != nil {
		s.t.Fatalf("create page %q: %v", p.Title, err)
	}
	return p
}

// checkGolden compares a body with testdata/<name>, or rewrites it under
// -update.
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
		t.Fatalf("golden %s: %v (run `go test ./internal/feeds/ -update`)", name, err)
	}
	if strings.TrimRight(string(want), "\n") != strings.TrimRight(got, "\n") {
		t.Errorf("golden %s does not match.\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

// TestFP_P25_RobotsAndSitemap: robots.txt points at the sitemap, and the
// sitemap is googlesitemap.cfm's document — the root hourly at 0.8 with
// the newest entry's stamp, every live entry with its own lastmod, every
// page weekly at 0.5 — with drafts and scheduled entries left out and the
// dates read in the blog's zone on both sides of a DST change.
func TestFP_P25_RobotsAndSitemap(t *testing.T) {
	s := newTestSite(t)

	// 2026-03-01 07:30Z is 2026-02-28 23:30 in PST (-08:00).
	s.entry(store.Entry{Title: "Winter entry", Alias: "winter-entry", Released: true,
		Username: "admin", Posted: time.Date(2026, time.March, 1, 7, 30, 0, 0, time.UTC)})
	// 2026-07-04 18:00Z is 2026-07-04 11:00 in PDT (-07:00).
	s.entry(store.Entry{Title: "Summer entry", Alias: "summer-entry", Released: true,
		Username: "admin", Posted: time.Date(2026, time.July, 4, 18, 0, 0, 0, time.UTC)})
	// An entry with no alias falls back to the id form, whose `&` has to
	// be escaped in the XML.
	s.entry(store.Entry{ID: "11111111-2222-4333-8444-555555555555", Title: "No alias", Released: true,
		Username: "admin", Posted: time.Date(2026, time.May, 5, 16, 0, 0, 0, time.UTC)})
	s.entry(store.Entry{Title: "Draft", Alias: "draft", Username: "admin",
		Posted: time.Date(2026, time.June, 1, 16, 0, 0, 0, time.UTC)})
	s.entry(store.Entry{Title: "Scheduled", Alias: "scheduled", Released: true, Username: "admin",
		Posted: time.Now().UTC().Add(72 * time.Hour)})

	s.page(store.Page{Title: "About", Alias: "about", Body: "<p>About.</p>", ShowLayout: true})
	s.page(store.Page{Title: "Contact details", Alias: "contact details", Body: "<p>Mail.</p>"})

	rec := s.get("/sitemap.xml")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /sitemap.xml = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/xml; charset=utf-8" {
		t.Errorf("sitemap Content-Type = %q", ct)
	}
	body := rec.Body.String()
	for _, absent := range []string{"draft", "scheduled"} {
		if strings.Contains(body, absent) {
			t.Errorf("the sitemap lists %q, which is not live:\n%s", absent, body)
		}
	}
	if !strings.Contains(body, "<loc>"+testBase+"/2026/2/28/winter-entry</loc>") {
		t.Errorf("the winter permalink is not the blog zone's date:\n%s", body)
	}
	if !strings.Contains(body, "<lastmod>2026-07-04T11:00:00-07:00</lastmod>") {
		t.Errorf("the summer lastmod is not the blog zone's time:\n%s", body)
	}
	checkGolden(t, "sitemap.xml", body)

	rec = s.get("/robots.txt")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /robots.txt = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Errorf("robots Content-Type = %q", ct)
	}
	robots := rec.Body.String()
	if strings.Contains(robots, "/mobile/") {
		t.Errorf("robots.txt still disallows the mobile skin:\n%s", robots)
	}
	if !strings.Contains(robots, "Sitemap: "+testBase+"/sitemap.xml") {
		t.Errorf("robots.txt does not point at the sitemap:\n%s", robots)
	}
	checkGolden(t, "robots.txt", robots)
}

// TestSitemapOnAnEmptyBlogIsValid: a blog with nothing in it still
// answers a well-formed document with its home page in it.
func TestSitemapOnAnEmptyBlogIsValid(t *testing.T) {
	s := newTestSite(t)
	rec := s.get("/sitemap.xml")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /sitemap.xml = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if strings.Count(body, "<url>") != 1 {
		t.Fatalf("an empty blog's sitemap has %d urls, want the root only:\n%s",
			strings.Count(body, "<url>"), body)
	}
	if !strings.Contains(body, "<loc>"+testBase+"/</loc>") ||
		!strings.Contains(body, "<changefreq>hourly</changefreq>") ||
		!strings.Contains(body, "<priority>0.8</priority>") {
		t.Errorf("the root url is not the as-is's:\n%s", body)
	}
	if err := xmlWellFormed(body); err != nil {
		t.Errorf("the document does not parse: %v\n%s", err, body)
	}
}
