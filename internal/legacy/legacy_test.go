package legacy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/4scottt/go-blogcfc/internal/config"
)

// testBase is deliberately not the httptest host: a redirect is built
// from BLOG_BASE_URL, never from the request (PLAN §6, FP O05).
const testBase = "http://blog.example"

func newTestMux(t *testing.T) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	New(&config.Config{BlogBaseURL: testBase + "/"}).Routes(mux)
	return mux
}

// TestFP_P27_LegacyCfmPathsRedirect301 is the whole legacy table: every
// BlogCFC `.cfm` URL answers 301 with the canonical URL, keeping the
// query string where the old page took one.
func TestFP_P27_LegacyCfmPathsRedirect301(t *testing.T) {
	mux := newTestMux(t)

	cases := []struct{ from, want string }{
		// index.cfm and its SES forms.
		{"/index.cfm", "/"},
		{"/index.cfm?startRow=11", "/?startRow=11"},
		{"/index.cfm?mode=entry&entry=abc", "/?mode=entry&entry=abc"},
		{"/index.cfm/", "/"},
		{"/index.cfm/2026/9/20/my-entry", "/2026/9/20/my-entry"},
		{"/index.cfm/2026/9", "/2026/9"},
		{"/index.cfm/support", "/support"},
		{"/index.cfm/postedby/admin", "/postedby/admin"},
		{"/index.cfm/caf%C3%A9", "/caf%C3%A9"},
		{"/index.cfm/2026/9/20/my-entry?killcomment=t0ken", "/2026/9/20/my-entry?killcomment=t0ken"},

		// Feeds, pages, search, sitemap.
		{"/rss.cfm", "/rss"},
		{"/rss.cfm?mode=full&version=1", "/rss?mode=full&version=1"},
		{"/page.cfm/about", "/page/about"},
		{"/page.cfm/about%20us", "/page/about%20us"},
		{"/search.cfm", "/search"},
		{"/search.cfm?search=coldfusion&category=abc", "/search?search=coldfusion&category=abc"},
		{"/googlesitemap.cfm", "/sitemap.xml"},

		// The admin is one screen now.
		{"/admin/index.cfm", "/admin/"},
		{"/admin/index.cfm?reinit=1", "/admin/?reinit=1"},
		{"/admin/entries.cfm", "/admin/"},
		{"/admin/settings.cfm", "/admin/"},
		{"/admin/moderate.cfm?approve=abc", "/admin/?approve=abc"},

		// Pages that took their subject in the query string.
		{"/print.cfm?id=ABC-123", "/print/ABC-123"},
		{"/print.cfm?id=", "/"},
		{"/print.cfm", "/"},
		{"/addcomment.cfm?id=ABC-123", "/comments/add/ABC-123"},
		{"/addcomment.cfm", "/"},

		// Enclosure downloads keep their shape.
		{"/download.cfm/ABC-123/show1.mp3", "/download/ABC-123/show1.mp3"},
		{"/download.cfm/ABC-123/podcasts/show1.mp3?online=1", "/download/ABC-123/podcasts/show1.mp3?online=1"},
	}

	for _, tc := range cases {
		t.Run(tc.from, func(t *testing.T) {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.from, nil))
			if rec.Code != http.StatusMovedPermanently {
				t.Fatalf("GET %s = %d, want 301", tc.from, rec.Code)
			}
			if got := rec.Header().Get("Location"); got != testBase+tc.want {
				t.Errorf("GET %s redirects to %q, want %q", tc.from, got, testBase+tc.want)
			}
		})
	}
}

// TestLegacyLeavesNonCfmAdminPathsAlone: only BlogCFC's admin pages by
// name are legacy URLs. Anything else under /admin/ (a new-style path, an
// unknown .cfm) is not this module's and gets a plain 404 from it alone, so
// the admin's own login gate answers in the real server.
func TestLegacyLeavesNonCfmAdminPathsAlone(t *testing.T) {
	mux := newTestMux(t)
	for _, path := range []string{"/admin/entries", "/admin/whatever", "/admin/nothing.cfm"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d (Location %q), want 404 from this module alone",
				path, rec.Code, rec.Header().Get("Location"))
		}
	}
}

// TestLegacyRedirectsOnlyAnswerGET: a POST to a legacy path is not this
// module's business.
func TestLegacyRedirectsOnlyAnswerGET(t *testing.T) {
	mux := newTestMux(t)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/index.cfm", nil))
	if rec.Code == http.StatusMovedPermanently {
		t.Error("a POST to /index.cfm was redirected")
	}
}
