// Package web is the public site: the home page, the archives, an entry,
// a category, a posted-by listing and the permalink grammar BlogCFC's SES
// URLs use (PLAN §8, §9 P01-P09). Comments, pods, search, feeds and the
// pages that go with them arrive with M2 and M3; until then the sidebar
// is empty and no view is counted.
package web

import (
	"bytes"
	"embed"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/4scottt/go-blogcfc/internal/config"
	"github.com/4scottt/go-blogcfc/internal/i18n"
	"github.com/4scottt/go-blogcfc/internal/mail"
	"github.com/4scottt/go-blogcfc/internal/store"
)

// Version is what the footer prints. The admin package carries its own;
// the two can be unified once the milestones stop moving separately.
const Version = "0.1.0-dev"

// Identity is how the public site learns who is logged in. The auth
// package implements it; a nil Identity means nobody ever is, which is
// what a test or a read-only deployment wants.
type Identity interface {
	Current(r *http.Request) *store.User
}

//go:embed templates/*.html
var templateFS embed.FS

// pageTemplates are the page bodies; each one defines "content" and is
// parsed together with the layout into its own set.
var pageTemplates = []string{"entries.html", "notfound.html"}

// Module holds the public site's dependencies and its parsed templates.
type Module struct {
	// Sidebar renders the pods column for one request. main.go sets it
	// from the pods package; while it is nil the layout leaves ul#sidebar
	// empty, which is what M1 and the tests want (PLAN §9 D01-D10).
	Sidebar func(r *http.Request) template.HTML
	// Mail is how contact, send and the subscribe pod send; main.go sets it
	// (a log sender until SMTP lands with M3). Nil means sending fails.
	Mail mail.Sender

	cfg      *config.Config
	store    *store.Store
	settings *config.Settings
	identity Identity
	bundle   *i18n.Bundle
	tmpl     map[string]*template.Template
}

// New builds the public site. It panics if the embedded templates do not
// parse, which is a build-time mistake, not a runtime one.
func New(cfg *config.Config, st *store.Store, settings *config.Settings, identity Identity) *Module {
	m := &Module{
		cfg:      cfg,
		store:    st,
		settings: settings,
		identity: identity,
		bundle:   i18n.New(settings.Locale()),
		tmpl:     map[string]*template.Template{},
	}
	for _, name := range pageTemplates {
		m.tmpl[name] = template.Must(template.New("layout.html").
			ParseFS(templateFS, "templates/layout.html", "templates/"+name))
	}
	return m
}

// Routes registers the public URL map (PLAN §8). Go's ServeMux ranks the
// overlaps: a literal segment beats a wildcard, so /postedby/{username}
// wins over /{alias}, and /health, /static/ and /admin/ beat the catch-all.
//
// The SES date forms (/{year}/{month}[/{day}[/{alias}]]) are served by the
// catch-all rather than by three wildcard patterns of their own: a pattern
// like "GET /{year}/{month}" overlaps the admin's "GET /admin/" subtree
// without either being more specific, which makes ServeMux panic at
// registration. "GET /" is strictly less specific than every subtree a
// later milestone registers, so those keep winning as they are added.
func (m *Module) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", m.handleHome)
	mux.HandleFunc("GET /postedby/{username}", m.handlePostedBy)
	mux.HandleFunc("GET /{alias}", m.handleCategoryByAlias)
	mux.HandleFunc("GET /", m.handleSES)
	m.routesEntry(mux)
	m.routesExtra(mux)
}

// reservedSegments are the first path segments later milestones own; a
// category alias may not be one of them (PLAN §18). Until those routes
// exist, asking for one is a 404 rather than a category lookup.
var reservedSegments = map[string]bool{
	"admin": true, "comments": true, "confirmsubscription": true, "contact": true,
	"download": true, "enclosures": true, "health": true, "images": true,
	"page": true, "postedby": true, "print": true, "robots.txt": true,
	"rss": true, "search": true, "send": true, "sitemap.xml": true,
	"slideshow": true, "static": true, "unsubscribe": true, "xmlrpc": true,
}

// base is the blog's absolute base URL without its trailing slash. Every
// link on every page is built from it (PLAN §6, FP O05).
func (m *Module) base() string { return strings.TrimRight(m.cfg.BlogBaseURL, "/") }

// loc is the blog's display zone.
func (m *Module) loc() *time.Location { return m.settings.Timezone() }

// currentUser returns the logged-in user, or nil.
func (m *Module) currentUser(r *http.Request) *store.User {
	if m.identity == nil {
		return nil
	}
	return m.identity.Current(r)
}

// adminView reports whether ?adminview=1 should show drafts and future
// entries: BlogCFC's getmode.cfm drops the released-only filter only for a
// logged-in admin, so an anonymous visitor asking for it changes nothing.
func (m *Module) adminView(r *http.Request) bool {
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("adminview"))) {
	case "1", "true", "yes":
		return m.currentUser(r) != nil
	default:
		return false
	}
}

// render writes one page. It renders into a buffer first so a template
// error cannot leave half a page on the wire.
func (m *Module) render(w http.ResponseWriter, page string, status int, data pageData) {
	t, ok := m.tmpl[page]
	if !ok {
		slog.Error("web: unknown template", "page", page)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout.html", data); err != nil {
		slog.Error("web: render failed", "page", page, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if _, err := buf.WriteTo(w); err != nil {
		slog.Debug("web: write failed", "error", err)
	}
}

// serverError logs and answers 500. A store failure is the platform's
// problem, never the visitor's to read.
func (m *Module) serverError(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("web: request failed", "path", r.URL.Path, "error", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

// notFound renders the plain 404 page inside the layout.
func (m *Module) notFound(w http.ResponseWriter, r *http.Request) {
	m.render(w, "notfound.html", http.StatusNotFound, m.newPage(r, "Not Found"))
}
