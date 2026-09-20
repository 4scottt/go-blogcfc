package web

import (
	"bytes"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strings"
)

// routesExtra registers the rest of the public map: search (/search and
// the /search/{term} shortcut), the contact form (/contact),
// email-this-entry (/send/{id}), the slideshow (/slideshow/{name}),
// enclosure downloads (/download/{id}/{file}) and the two file trees
// those downloads point at, /enclosures/{file} and /images/uploads/…
// (PLAN §8, §9 P19-P24).
//
// The forms answer both methods: BlogCFC's search.cfm, contact.cfm and
// send.cfm were one page that posted to itself, and the layout's header
// form is a GET, so both arrive at the same handler.
func (m *Module) routesExtra(mux *http.ServeMux) {
	mux.HandleFunc("GET /search", m.handleSearch)
	mux.HandleFunc("POST /search", m.handleSearch)
	mux.HandleFunc("GET /search/{term}", m.handleSearch)
	mux.HandleFunc("GET /contact", m.handleContact)
	mux.HandleFunc("POST /contact", m.handleContact)
	mux.HandleFunc("GET /send/{id}", m.handleSend)
	mux.HandleFunc("POST /send/{id}", m.handleSend)
	mux.HandleFunc("GET /slideshow/{name}", m.handleSlideshow)
	mux.HandleFunc("GET /download/{id}/{file}", m.handleDownload)
	mux.HandleFunc("GET /enclosures/{file}", m.handleEnclosureFile)
	mux.HandleFunc("GET /images/uploads/{path...}", m.handleUploadFile)
}

// extraPages are this package's page bodies. web.go's New parses its own
// list and this one is parsed here, so neither file has to know about the
// other; the layout is shared and the embed pattern already covers both.
var extraPages = []string{"search.html", "contact.html", "send.html", "slideshow.html"}

// extraTemplates is parsed once at start. A template that does not parse
// is a build-time mistake, as it is in New.
var extraTemplates = func() map[string]*template.Template {
	out := make(map[string]*template.Template, len(extraPages))
	for _, name := range extraPages {
		out[name] = template.Must(template.New("layout.html").
			ParseFS(templateFS, "templates/layout.html", "templates/"+name))
	}
	return out
}()

// renderExtra writes one of this package's pages. Like render it goes
// through a buffer first, so a template error cannot leave half a page on
// the wire; it takes any view struct (each page embeds pageData, whose
// fields the layout reads by promotion).
func (m *Module) renderExtra(w http.ResponseWriter, page string, status int, data any) {
	t, ok := extraTemplates[page]
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

// requestIP is the visitor's address for the contact mail and the
// download log: BlogCFC read CGI.REMOTE_ADDR, which behind our reverse
// proxy is the proxy. The first X-Forwarded-For hop wins when the header
// is there, the connection's host otherwise.
func requestIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if first := strings.TrimSpace(strings.Split(fwd, ",")[0]); first != "" {
			return first
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// emailPattern is BlogCFC's isEmail (org/camden/blog/utils.cfc) with its
// 2007 TLD list replaced by "two letters or more": the list predates
// every new gTLD and would refuse a perfectly good address today.
var emailPattern = regexp.MustCompile(`^['_a-z0-9-]+(\.['_a-z0-9-]+)*(\+['_a-z0-9-]+)*@[a-z0-9-]+(\.[a-z0-9-]+)*\.[a-z]{2,}$`)

// looksLikeEmail keeps the as-is length bounds: 64 characters of local
// part, 255 of domain.
func looksLikeEmail(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	local, domain, ok := strings.Cut(s, "@")
	if !ok || len(local) > 64 || len(domain) > 255 {
		return false
	}
	return emailPattern.MatchString(s)
}

// mailFrom is the envelope sender for the mail these forms send. BlogCFC
// put the visitor's own address in From, which a modern receiver rejects
// on SPF grounds; the blog's own `failto` (or the owner) sends, and the
// visitor's address stays in the body, where the owner still sees it.
func (m *Module) mailFrom() string {
	if from := strings.TrimSpace(m.settings.FailTo()); from != "" {
		return from
	}
	return strings.TrimSpace(m.settings.OwnerEmail())
}

// formValue reads a field from the query or the posted body, trimmed.
func formValue(r *http.Request, name string) string {
	return strings.TrimSpace(r.FormValue(name))
}
