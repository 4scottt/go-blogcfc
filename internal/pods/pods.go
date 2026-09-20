// Package pods is the sidebar: BlogCFC's ten pods (client/includes/pods),
// rendered in the order and with the visibility the `pods` setting gives
// them (PLAN §9 D01-D10, §10 "Pods", §12).
//
// The public site owns the column; this package owns what goes in it.
// main.go hands web.Module.Sidebar this module's Sidebar, so web never
// imports pods and pods never renders a page of the site.
package pods

import (
	"bytes"
	"context"
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

// The pod names the `pods` setting uses. They are BlogCFC's file names
// without the `.cfm`, so an operator's old pods.xml order still reads
// (feed.cfm is dropped, PLAN §9 D).
const (
	Calendar        = "calendar"
	Archives        = "archives"
	MonthlyArchives = "monthlyarchives"
	Recent          = "recent"
	RecentComments  = "recentcomments"
	Search          = "search"
	Subscribe       = "subscribe"
	TagCloud        = "tagcloud"
	RSS             = "rss"
	Pages           = "pages"
)

// Defaults are pods.xml's list, in its sortorder: what the sidebar shows
// when the `pods` setting is empty (PLAN §9 D10).
func Defaults() []config.Pod {
	return []config.Pod{
		{Name: Calendar, Show: true, Order: 1},
		{Name: Subscribe, Show: true, Order: 2},
		{Name: RecentComments, Show: true, Order: 3},
		{Name: Recent, Show: true, Order: 4},
		{Name: Archives, Show: true, Order: 5},
	}
}

//go:embed templates/*.html
var templateFS embed.FS

// Module renders the pods. It is safe for concurrent use: everything it
// keeps is read-only after New.
type Module struct {
	cfg      *config.Config
	store    *store.Store
	settings *config.Settings
	sender   mail.Sender
	tmpl     *template.Template

	// renderers maps a pod name to what draws it. A name with no entry
	// is skipped, which is how an unknown pod in the setting is ignored.
	renderers map[string]func(*podCtx) (string, template.HTML)
}

// New builds the module. It panics if the embedded templates do not
// parse, which is a build-time mistake, not a runtime one.
func New(cfg *config.Config, st *store.Store, settings *config.Settings, sender mail.Sender) *Module {
	m := &Module{
		cfg:      cfg,
		store:    st,
		settings: settings,
		sender:   sender,
		tmpl:     template.Must(template.ParseFS(templateFS, "templates/*.html")),
	}
	m.renderers = map[string]func(*podCtx) (string, template.HTML){
		Calendar:        m.calendarPod,
		Archives:        m.archivesPod,
		MonthlyArchives: m.monthlyArchivesPod,
		Recent:          m.recentPod,
		RecentComments:  m.recentCommentsPod,
		Search:          m.searchPod,
		Subscribe:       m.subscribePod,
		TagCloud:        m.tagCloudPod,
		RSS:             m.rssPod,
		Pages:           m.pagesPod,
	}
	return m
}

// podCtx is what every pod renderer is given: the request it draws for,
// and the blog's base URL, zone and strings.
type podCtx struct {
	ctx    context.Context
	req    *http.Request
	base   string
	loc    *time.Location
	bundle *i18n.Bundle
	now    time.Time
}

func (m *Module) newCtx(r *http.Request) *podCtx {
	loc := m.settings.Timezone()
	return &podCtx{
		ctx:    r.Context(),
		req:    r,
		base:   m.base(),
		loc:    loc,
		bundle: i18n.New(m.settings.Locale()),
		now:    time.Now().In(loc),
	}
}

// base is the blog's absolute base URL without its trailing slash. Every
// link a pod draws is built from it, never from the request's Host
// (PLAN §6, FP O05).
func (m *Module) base() string { return strings.TrimRight(m.cfg.BlogBaseURL, "/") }

// Sidebar renders every visible pod, in order, as the `li.block` items
// that go inside the layout's `ul#sidebar` (PLAN §12).
func (m *Module) Sidebar(r *http.Request) template.HTML {
	return m.sidebar(m.newCtx(r))
}

func (m *Module) sidebar(c *podCtx) template.HTML {
	list := m.settings.Pods()
	if len(list) == 0 {
		list = Defaults()
	}
	var out strings.Builder
	for _, p := range list {
		if !p.Show {
			continue
		}
		render, ok := m.renderers[podName(p.Name)]
		if !ok {
			continue // an unknown pod is ignored, not an error
		}
		title, content := render(c)
		out.WriteString(string(m.box(title, content)))
	}
	return template.HTML(out.String()) //nolint:gosec // every part is template-escaped
}

// podName normalises a name from the setting: case and spaces do not
// matter, and BlogCFC's own `calendar.cfm` spelling still resolves.
func podName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	return strings.TrimSuffix(name, ".cfm")
}

// box wraps one pod in the sidebar's furniture (PLAN §12).
func (m *Module) box(title string, content template.HTML) template.HTML {
	return m.exec("box", struct {
		Title   string
		Content template.HTML
	}{title, content})
}

// exec runs one template into a string. A template that fails to render
// costs its pod, not the page.
func (m *Module) exec(name string, data any) template.HTML {
	var buf bytes.Buffer
	if err := m.tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		slog.Warn("pods: template failed", "template", name, "error", err)
		return ""
	}
	return template.HTML(buf.String()) //nolint:gosec // the template escapes its data
}

// fail logs a pod's data error and leaves it empty: a database hiccup
// loses the sidebar's widget, never the page around it.
func fail(pod string, err error) template.HTML {
	slog.Warn("pods: pod failed", "pod", pod, "error", err)
	return ""
}

// text is a string the bundle may or may not carry. BlogCFC hardcoded a
// few pod strings in English (the tag cloud's title, the pages pod's
// "NAVIGATION" and "Home", the subscribe pod's messages) and shipped no
// keys for them; this uses a key when a bundle defines one - so a
// translator can still add it - and the as-is English otherwise.
func text(b *i18n.Bundle, key, asIs string) string {
	if b.Has(key) {
		return b.T(key)
	}
	return asIs
}
