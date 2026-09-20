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
	// The entry header and its footer metadata line, and the whole head:
	// the three fragments the look package holds as goldens (P10, P11, P26).
	headerBlock   = regexp.MustCompile(`(?s)<div class="post-header">.*?</div>`)
	metadataBlock = regexp.MustCompile(`(?s)<p class="post-metadata">.*?</p>`)
	headBlock     = regexp.MustCompile(`(?s)<head>.*?</head>`)
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
		`<link rel="alternate" type="application/rss+xml" title="BlogCFC" href="` + testBase + `/rss" />`,
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

// TestFP_P10_EntryHeaderMarkup is P10: the entry header is the as-is
// block -- a linked title, the month/day badge, the author and the
// category links, and a comment count anchored at the permalink's
// #comments (PLAN §12, §9 P10).
func TestFP_P10_EntryHeaderMarkup(t *testing.T) {
	s := newTestSite(t, nil)
	s.user("ray", "Raymond Camden")
	cf := s.category("11111111-1111-4111-8111-111111111111", "ColdFusion", "coldfusion")
	go2 := s.category("22222222-2222-4222-8222-222222222222", "Go", "go")
	e := s.entry(store.Entry{
		ID: "77777777-7777-4777-8777-777777777777", Title: "Header Markup", Alias: "header-markup",
		Body: "<p>Body.</p>", Posted: utc(2026, 4, 9, 11, 5), Username: "ray", Released: true,
	})
	s.categorise(e.ID, cf.ID, go2.ID)

	body := s.getOK("/2026/4/9/header-markup")
	header := headerBlock.FindString(body)
	if header == "" {
		t.Fatalf("no post-header block on the entry page:\n%s", body)
	}
	permalink := testBase + "/2026/4/9/header-markup"
	for _, want := range []string{
		`<h3 class="post-title"><a href="` + permalink + `">Header Markup</a></h3>`,
		`<p class="post-date">`,
		`<span class="month">Apr</span>`,
		`<span class="day">9</span>`,
		`<p class="post-author">`,
		`posted by <a href="` + testBase + `/postedby/ray">ray</a> in `,
		`<a href="` + testBase + `/coldfusion">ColdFusion</a>`,
		`<a href="` + testBase + `/go">Go</a>`,
		`| <a href="` + permalink + `#comments" class="comments">0 Comments</a>`,
	} {
		if !strings.Contains(header, want) {
			t.Errorf("the entry header is missing %q:\n%s", want, header)
		}
	}
	checkGolden(t, "entry_header.html", header)
}

// TestFP_P11_EntryFooterMetadataMarkup is P11: the footer line carries the
// posted date and time, the view count, the comment count, a Print link,
// and a Download link only when the entry has an enclosure.
func TestFP_P11_EntryFooterMetadataMarkup(t *testing.T) {
	s := newTestSite(t, nil)
	s.user("ray", "Raymond Camden")
	plain := s.entry(store.Entry{
		ID: "33333333-3333-4333-8333-333333333333", Title: "No Attachment", Alias: "no-attachment",
		Body: "<p>Body.</p>", Posted: utc(2026, 4, 10, 16, 45), Username: "ray", Released: true, Views: 7,
	})
	withFile := s.entry(store.Entry{
		ID: "44444444-4444-4444-8444-444444444444", Title: "With Attachment", Alias: "with-attachment",
		Body: "<p>Body.</p>", Posted: utc(2026, 4, 11, 8, 0), Username: "ray", Released: true, Views: 3,
		Enclosure: "/data/enclosures/episode 12.mp3", FileSize: 1024, MimeType: "audio/mpeg",
	})

	meta := metadataBlock.FindString(s.getOK("/2026/4/10/no-attachment"))
	if meta == "" {
		t.Fatal("no post-metadata line on the entry without an enclosure")
	}
	if !strings.Contains(meta, "posted on April 10, 2026 at 4:45 PM and has received 7 views") {
		t.Errorf("the metadata line does not read right:\n%s", meta)
	}
	if !strings.Contains(meta, `<a href="`+testBase+`/print/`+plain.ID+`" rel="nofollow">Print this entry.</a>`) {
		t.Errorf("the metadata line has no print link:\n%s", meta)
	}
	if strings.Contains(meta, "Download") {
		t.Errorf("an entry without an enclosure offers a download:\n%s", meta)
	}
	checkGolden(t, "entry_metadata_plain.html", meta)

	meta = metadataBlock.FindString(s.getOK("/2026/4/11/with-attachment"))
	if meta == "" {
		t.Fatal("no post-metadata line on the entry with an enclosure")
	}
	want := `<a href="` + testBase + `/download/` + withFile.ID + `/episode%2012.mp3">Download attachment.</a>`
	if !strings.Contains(meta, want) {
		t.Errorf("the metadata line is missing %q:\n%s", want, meta)
	}
	checkGolden(t, "entry_metadata_enclosure.html", meta)
}

// TestFP_P26_LayoutTitleMetaRssLinkFrameBuster is P26: the head carries
// the blog title with the mode's additional title, the description and
// keywords from settings, an RSS alternate link titled with the blog, and
// the frame buster the as-is layout ran on load.
func TestFP_P26_LayoutTitleMetaRssLinkFrameBuster(t *testing.T) {
	s := newTestSite(t, nil)
	s.setSetting("blogtitle", "Ray's Blog")
	s.setSetting("blogdescription", "A blog about ColdFusion.")
	s.setSetting("blogkeywords", "coldfusion, blogcfc, go")
	s.user("ray", "Raymond Camden")
	s.entry(store.Entry{
		ID: "55555555-5555-4555-8555-555555555555", Title: "Titled Entry", Alias: "titled-entry",
		Body: "<p>Body.</p>", Posted: utc(2026, 4, 12, 10, 0), Username: "ray", Released: true,
	})

	home := headBlock.FindString(s.getOK("/"))
	for _, want := range []string{
		`<title>Ray&#39;s Blog</title>`,
		`<meta name="description" content="A blog about ColdFusion." />`,
		`<meta name="keywords" content="coldfusion, blogcfc, go" />`,
		`<link rel="alternate" type="application/rss+xml" title="Ray&#39;s Blog" href="` + testBase + `/rss" />`,
		`<link rel="stylesheet" type="text/css" href="` + testBase + `/static/css/site.css" />`,
		"top.location = self.location",
	} {
		if !strings.Contains(home, want) {
			t.Errorf("the head is missing %q:\n%s", want, home)
		}
	}
	checkGolden(t, "head_home.html", home)

	entry := headBlock.FindString(s.getOK("/2026/4/12/titled-entry"))
	if !strings.Contains(entry, `<title>Ray&#39;s Blog - Titled Entry</title>`) {
		t.Errorf("the entry head has no additional title:\n%s", entry)
	}
	checkGolden(t, "head_entry.html", entry)
}
