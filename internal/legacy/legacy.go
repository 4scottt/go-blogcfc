// Package legacy answers BlogCFC's `.cfm` URLs with a permanent redirect
// to the rewrite's canonical form (PLAN §8, §9 P27). Ten years of links,
// feed readers and search results point at `index.cfm/2011/9/20/alias`;
// they keep working, and they stop being two URLs for one page.
package legacy

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/4scottt/go-blogcfc/internal/config"
)

// Module holds the base every redirect is built from. Like every other
// URL in the rewrite it comes from BLOG_BASE_URL, never from the request's
// Host (PLAN §6, FP O05).
type Module struct {
	base string
}

// New builds the redirect table.
func New(cfg *config.Config) *Module {
	return &Module{base: strings.TrimRight(cfg.BlogBaseURL, "/")}
}

// Routes registers the legacy paths. Each one is a literal or a wildcard
// deeper than the public site's catch-all, so ServeMux prefers it.
func (m *Module) Routes(mux *http.ServeMux) {
	// index.cfm and every SES path that hung off it.
	mux.HandleFunc("GET /index.cfm", m.redirectTo("/"))
	mux.HandleFunc("GET /index.cfm/{rest...}", m.redirectRest("/index.cfm/", "/"))

	mux.HandleFunc("GET /rss.cfm", m.redirectTo("/rss"))
	mux.HandleFunc("GET /search.cfm", m.redirectTo("/search"))
	mux.HandleFunc("GET /googlesitemap.cfm", m.redirectTo("/sitemap.xml"))
	mux.HandleFunc("GET /page.cfm/{alias}", m.redirectSegment("/page/", "alias"))

	// The admin is one screen now: any old admin page lands on it.
	mux.HandleFunc("GET /admin/index.cfm", m.redirectTo("/admin/"))
	// The rest of the as-is admin pages, by name: a wildcard here would take
	// every single-segment /admin/ path away from the admin's login gate.
	for _, page := range adminPages {
		mux.HandleFunc("GET /admin/"+page+".cfm", m.adminPage)
	}

	// Two pages took their subject in the query string.
	mux.HandleFunc("GET /print.cfm", m.redirectQueryID("/print/"))
	mux.HandleFunc("GET /addcomment.cfm", m.redirectQueryID("/comments/add/"))

	// download.cfm/{id}/{file} keeps its shape, minus the .cfm.
	mux.HandleFunc("GET /download.cfm/{id}/{file...}", m.redirectRest("/download.cfm/", "/download/"))
}

// moved sends the one answer this package has: 301 to an absolute URL
// under the blog's base.
func (m *Module) moved(w http.ResponseWriter, r *http.Request, path string) {
	http.Redirect(w, r, m.base+path, http.StatusMovedPermanently)
}

// keepQuery appends the request's query string, which the old links carry
// (`?startRow=`, `?mode=`, `rss.cfm?mode=full`).
func keepQuery(path string, r *http.Request) string {
	if r.URL.RawQuery == "" {
		return path
	}
	return path + "?" + r.URL.RawQuery
}

// redirectTo answers a fixed path, query and all.
func (m *Module) redirectTo(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m.moved(w, r, keepQuery(path, r))
	}
}

// redirectRest moves everything after a prefix to a new prefix. It reads
// the escaped path rather than the wildcard's value, so an alias with an
// escaped character stays escaped in the Location header.
func (m *Module) redirectRest(oldPrefix, newPrefix string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.EscapedPath(), oldPrefix)
		m.moved(w, r, keepQuery(newPrefix+rest, r))
	}
}

// redirectSegment moves one wildcard segment under a new prefix.
func (m *Module) redirectSegment(prefix, name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m.moved(w, r, keepQuery(prefix+url.PathEscape(r.PathValue(name)), r))
	}
}

// redirectQueryID turns `?id=` into a path segment. Without an id there is
// nothing to show, so the reader goes to the home page, as BlogCFC's own
// `cflocation` did.
func (m *Module) redirectQueryID(prefix string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if id == "" {
			m.moved(w, r, "/")
			return
		}
		m.moved(w, r, prefix+url.PathEscape(id))
	}
}

// adminPages are BlogCFC 5.9.8's admin templates (client/admin/*.cfm) other
// than index.cfm, which has its own line above.
var adminPages = []string{
	"categories", "category", "comment", "comments", "downloads", "entries",
	"entry_comments", "entry", "filemanager", "imgbrowse", "imgwin",
	"latestversioncheck", "login", "mailsubscribers", "moderate", "notify",
	"page", "pages", "pod", "podform", "pods", "proxy", "settings", "showpods",
	"slideshow", "slideshows", "stats", "stats2", "statsbyyear", "subscribers",
	"textblock", "textblocks", "updatepassword", "user", "users",
}

// adminPage lands any of BlogCFC's admin pages on the dashboard.
func (m *Module) adminPage(w http.ResponseWriter, r *http.Request) {
	m.moved(w, r, keepQuery("/admin/", r))
}
