package admin

import (
	"bytes"
	"embed"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"path"

	"github.com/4scottt/go-blogcfc/internal/config"
	"github.com/4scottt/go-blogcfc/internal/store"
)

//go:embed templates/*.html
var templateFS embed.FS

// pageTemplates holds one parsed template per page, each already joined
// with layout.html. A page file defines `content`; layout.html calls it.
var pageTemplates = parsePages()

func parsePages() map[string]*template.Template {
	names, err := fs.Glob(templateFS, "templates/*.html")
	if err != nil {
		panic("admin: glob templates: " + err.Error())
	}
	out := make(map[string]*template.Template, len(names))
	for _, name := range names {
		base := path.Base(name)
		if base == "layout.html" {
			continue
		}
		// ParseFS names templates by base name, so the first one parsed,
		// layout.html, is the one Execute runs.
		out[base] = template.Must(template.New("layout.html").ParseFS(templateFS, "templates/layout.html", name))
	}
	return out
}

// pageData is what every admin template is given. Page carries whatever
// the one screen needs; the rest is the frame, so a later package builds
// this with newPageData and fills Page only.
type pageData struct {
	// Title is the <title> and the #header heading: the page's one
	// distinctive heading, which the walk and the Pilot key on.
	Title     string
	User      *store.User
	Settings  *config.Settings
	BlogTitle string
	Menu      []MenuGroup
	// Flash is a one-off notice shown above the content in .banner.
	Flash string
	Page  any
}

// newPageData builds the frame for a request: the signed-in user (nil on
// the login page) and the menu that user may see.
func (m *Module) newPageData(r *http.Request, title string) pageData {
	u := m.sessions.Current(r)
	return pageData{
		Title:     title,
		User:      u,
		Settings:  m.settings,
		BlogTitle: m.settings.BlogTitle(),
		// The menu is built per render, so the moderation count beside
		// Moderate is the live one (PLAN §9 A14).
		Menu: m.menuFor(r.Context(), u),
	}
}

// render writes a page with status 200.
func render(w http.ResponseWriter, name string, data pageData) {
	renderStatus(w, http.StatusOK, name, data)
}

// renderStatus writes a page with a status code. The template runs into a
// buffer first, so a template error is a 500 and not half a page.
func renderStatus(w http.ResponseWriter, status int, name string, data pageData) {
	t, ok := pageTemplates[name]
	if !ok {
		slog.Error("admin: no such template", "template", name)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		slog.Error("admin: render failed", "template", name, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}
