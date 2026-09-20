// Package feeds serves the machine-readable views of the blog. This file
// holds the sitemap and robots.txt (PLAN §8, §9 P25); the RSS feeds join
// it in a later milestone.
package feeds

import (
	"bytes"
	"encoding/xml"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/4scottt/go-blogcfc/internal/config"
	"github.com/4scottt/go-blogcfc/internal/store"
	"github.com/4scottt/go-blogcfc/internal/web"
)

// sitemapTime is BlogCFC's googlesitemap.cfm stamp: the date and time
// followed by the blog zone's offset, never a bare `Z`.
const sitemapTime = "2006-01-02T15:04:05-07:00"

// Sitemap serves /sitemap.xml and /robots.txt.
type Sitemap struct {
	cfg      *config.Config
	store    *store.Store
	settings *config.Settings
}

// NewSitemap builds the module.
func NewSitemap(cfg *config.Config, st *store.Store, settings *config.Settings) *Sitemap {
	return &Sitemap{cfg: cfg, store: st, settings: settings}
}

// Routes registers both files at the root, where crawlers look for them.
func (m *Sitemap) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /sitemap.xml", m.handleSitemap)
	mux.HandleFunc("GET /robots.txt", m.handleRobots)
}

// base is the blog's own URL, without a trailing slash.
func (m *Sitemap) base() string { return strings.TrimRight(m.cfg.BlogBaseURL, "/") }

// handleSitemap answers the sitemap 0.84 document googlesitemap.cfm
// answered: the home page hourly at priority 0.8, every live entry at its
// permalink with the entry's own `posted` as its lastmod, and every
// static page weekly at 0.5.
func (m *Sitemap) handleSitemap(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	entries, err := m.store.EntriesForSitemap(ctx)
	if err != nil {
		slog.Error("sitemap: entries", "error", err)
		http.Error(w, "sitemap unavailable", http.StatusInternalServerError)
		return
	}
	pages, err := m.store.ListPages(ctx)
	if err != nil {
		slog.Error("sitemap: pages", "error", err)
		http.Error(w, "sitemap unavailable", http.StatusInternalServerError)
		return
	}

	loc := m.settings.Timezone()
	// The root's lastmod is the newest entry, or now on an empty blog.
	root := time.Now()
	if len(entries) > 0 {
		root = entries[0].Posted
	}

	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<urlset xmlns="http://www.google.com/schemas/sitemap/0.84"` + "\n")
	b.WriteString("\t" + `xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"` + "\n")
	b.WriteString("\t" + `xsi:schemaLocation="http://www.google.com/schemas/sitemap/0.84` + "\n")
	b.WriteString("\t" + `http://www.google.com/schemas/sitemap/0.84/sitemap.xsd">` + "\n")

	b.WriteString("\t<url>\n")
	b.WriteString("\t\t<loc>" + xmlEscape(m.base()+"/") + "</loc>\n")
	b.WriteString("\t\t<lastmod>" + root.In(loc).Format(sitemapTime) + "</lastmod>\n")
	b.WriteString("\t\t<changefreq>hourly</changefreq>\n")
	b.WriteString("\t\t<priority>0.8</priority>\n")
	b.WriteString("\t</url>\n")

	for _, e := range entries {
		b.WriteString("\t<url>\n")
		b.WriteString("\t\t<loc>" + xmlEscape(web.EntryURL(m.base(), e, loc)) + "</loc>\n")
		b.WriteString("\t\t<lastmod>" + e.Posted.In(loc).Format(sitemapTime) + "</lastmod>\n")
		b.WriteString("\t</url>\n")
	}

	for _, p := range pages {
		b.WriteString("\t<url>\n")
		b.WriteString("\t\t<loc>" + xmlEscape(m.base()+"/page/"+pathEscape(p.Alias)) + "</loc>\n")
		b.WriteString("\t\t<priority>0.5</priority>\n")
		b.WriteString("\t\t<changefreq>weekly</changefreq>\n")
		b.WriteString("\t\t<lastmod>" + root.In(loc).Format(sitemapTime) + "</lastmod>\n")
		b.WriteString("\t</url>\n")
	}

	b.WriteString("</urlset>\n")

	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	_, _ = w.Write(b.Bytes())
}

// xmlEscape escapes a URL for a text node, as googlesitemap.cfm's
// xmlFormat does: an `&` in an id-form permalink must not break the
// document.
func xmlEscape(s string) string {
	var b bytes.Buffer
	if err := xml.EscapeText(&b, []byte(s)); err != nil {
		return ""
	}
	return b.String()
}
