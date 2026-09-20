package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/4scottt/go-blogcfc/internal/config"
	"github.com/4scottt/go-blogcfc/internal/mail"
	"github.com/4scottt/go-blogcfc/internal/store"
	"github.com/4scottt/go-blogcfc/internal/testdb"
)

// extraSite is one wired-up public site for the search, contact, send,
// slideshow and download pages: a real database, a recording mail sender
// and a DATA_DIR of its own under t.TempDir().
type extraSite struct {
	t        *testing.T
	store    *store.Store
	settings *config.Settings
	mail     *mail.Recorder
	dataDir  string
	handler  http.Handler
}

// newExtraSite builds the module the way serve does, with the two seams
// these pages need: cfg.DataDir and Module.Mail.
func newExtraSite(t *testing.T) *extraSite {
	t.Helper()
	st := testdb.New(t)
	dir := t.TempDir()
	cfg := &config.Config{BlogBaseURL: testBase, Port: 8080, DataDir: dir}
	settings := config.NewSettings(st, cfg)
	if err := settings.Reload(context.Background()); err != nil {
		t.Fatalf("settings reload: %v", err)
	}
	recorder := &mail.Recorder{}
	m := New(cfg, st, settings, nil)
	m.Mail = recorder
	mux := http.NewServeMux()
	m.Routes(mux)
	return &extraSite{t: t, store: st, settings: settings, mail: recorder, dataDir: dir, handler: mux}
}

// get runs one GET.
func (s *extraSite) get(path string) *httptest.ResponseRecorder {
	s.t.Helper()
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// getOK runs one GET and fails unless it answers 200.
func (s *extraSite) getOK(path string) string {
	s.t.Helper()
	rec := s.get(path)
	if rec.Code != http.StatusOK {
		s.t.Fatalf("GET %s = %d, want 200", path, rec.Code)
	}
	return rec.Body.String()
}

// post runs one POST of a form.
func (s *extraSite) post(path string, form url.Values, headers map[string]string) *httptest.ResponseRecorder {
	s.t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	return rec
}

// setSetting writes one setting and reloads the cache.
func (s *extraSite) setSetting(key, value string) {
	s.t.Helper()
	if err := s.settings.Set(context.Background(), map[string]string{key: value}); err != nil {
		s.t.Fatalf("set %s: %v", key, err)
	}
}

// entry inserts one entry through the store.
func (s *extraSite) entry(e store.Entry) store.Entry {
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
func (s *extraSite) category(id, name, alias string) store.Category {
	s.t.Helper()
	c := store.Category{ID: id, Name: name, Alias: alias}
	if err := s.store.CreateCategory(context.Background(), &c); err != nil {
		s.t.Fatalf("create category %q: %v", name, err)
	}
	return c
}

// categorise links an entry to categories.
func (s *extraSite) categorise(entryID string, catIDs ...string) {
	s.t.Helper()
	if err := s.store.SetEntryCategories(context.Background(), entryID, catIDs); err != nil {
		s.t.Fatalf("set entry categories: %v", err)
	}
}

// author inserts one user, so an entry has somebody to belong to.
func (s *extraSite) author(username, name string) {
	s.t.Helper()
	u := &store.User{Username: username, Name: name, PasswordHash: "$2a$10$notarealhash"}
	if err := s.store.CreateUser(context.Background(), u, nil); err != nil {
		s.t.Fatalf("create user %q: %v", username, err)
	}
}

// writeDataFile puts a file under DATA_DIR, making its directory.
func (s *extraSite) writeDataFile(rel, content string) string {
	s.t.Helper()
	path := filepath.Join(s.dataDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		s.t.Fatalf("mkdir for %s: %v", rel, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		s.t.Fatalf("write %s: %v", rel, err)
	}
	return path
}

// mustContain fails unless every fragment is on the page.
func mustContain(t *testing.T, what, body string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Errorf("%s is missing %q:\n%s", what, want, body)
		}
	}
}

// TestExtraEmailValidationFollowsTheAsIs covers the address check the
// contact and send forms share: BlogCFC's isEmail, minus its 2007 list
// of top-level domains (PLAN §9 P21, P22).
func TestExtraEmailValidationFollowsTheAsIs(t *testing.T) {
	for _, good := range []string{"ray@camdenfamily.com", "a.b+c@sub.example.co.uk", "o'brien@example.dev"} {
		if !looksLikeEmail(good) {
			t.Errorf("%q should be a valid address", good)
		}
	}
	for _, bad := range []string{"", "ray", "ray@", "@example.com", "ray@example", "ray @example.com",
		strings.Repeat("a", 65) + "@example.com"} {
		if looksLikeEmail(bad) {
			t.Errorf("%q should not be a valid address", bad)
		}
	}
}

// TestExtraRequestIPPrefersTheForwardedHop is what the contact mail and
// the download log record for a visitor behind the platform's proxy.
func TestExtraRequestIPPrefersTheForwardedHop(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.5:44321"
	if got := requestIP(r); got != "10.0.0.5" {
		t.Errorf("without a header the IP is %q, want the connection's host", got)
	}
	r.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.1")
	if got := requestIP(r); got != "203.0.113.9" {
		t.Errorf("with X-Forwarded-For the IP is %q, want the first hop", got)
	}
}
