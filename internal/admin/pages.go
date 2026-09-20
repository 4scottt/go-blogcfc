package admin

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	renderpkg "github.com/4scottt/go-blogcfc/internal/render"
	"github.com/4scottt/go-blogcfc/internal/store"
)

// maxPageAlias is what the alias column holds (0001_schema.sql). The
// as-is form said 100 and then saved `left(alias,50)`, losing the rest
// without a word (admin/page.cfm); this refuses the save instead.
const maxPageAlias = 100

// maxPageTitle is the title column's width.
const maxPageTitle = 255

// pagesPage is pages.html's own data.
type pagesPage struct {
	Rows []pageRow
}

// pageRow is one line of the list: what the as-is datatable showed
// (title and the page's public URL) plus the layout flag, which the
// as-is only revealed inside the editor.
type pageRow struct {
	ID         string
	Title      string
	Alias      string
	ShowLayout bool
	URL        string
}

// pagesList is GET /admin/pages (PLAN §9 A21, admin/pages.cfm).
func (m *Module) pagesList(w http.ResponseWriter, r *http.Request) {
	pages, err := m.store.ListPages(r.Context())
	if err != nil {
		m.serverError(w, r, err)
		return
	}
	rows := make([]pageRow, 0, len(pages))
	for _, p := range pages {
		rows = append(rows, pageRow{
			ID: p.ID, Title: p.Title, Alias: p.Alias, ShowLayout: p.ShowLayout,
			URL: m.base() + "/page/" + p.Alias,
		})
	}
	data := m.newPageData(r, "Pages")
	data.Flash = savedDeletedFlash(r, "Page")
	data.Page = pagesPage{Rows: rows}
	render(w, "pages.html", data)
}

// base is the blog's absolute base URL without a trailing slash: every
// link the admin shows an operator is built from it, never from the
// request's Host (PLAN §6).
func (m *Module) base() string { return strings.TrimRight(m.cfg.BlogBaseURL, "/") }

// savedDeletedFlash is the banner the content screens redirect with.
func savedDeletedFlash(r *http.Request, noun string) string {
	q := r.URL.Query()
	switch {
	case q.Has("saved"):
		return noun + " saved."
	case q.Has("deleted"):
		return noun + " deleted."
	default:
		return ""
	}
}

// pageFormPage is page.html's own data.
type pageFormPage struct {
	IsNew      bool
	Action     string
	DeleteURL  string
	Errors     []string
	Title      string
	Alias      string
	Body       string
	ShowLayout bool
	URL        string
	Categories []pageCategory
}

// pageCategory is one option of the categories multiselect.
type pageCategory struct {
	ID       string
	Name     string
	Selected bool
}

// pageForm is GET /admin/pages/{id} and /admin/pages/new.
func (m *Module) pageForm(w http.ResponseWriter, r *http.Request) {
	id := contentPathID(r)
	p := pageFormPage{IsNew: id == "", Action: "/admin/pages/new", ShowLayout: true}
	var selected []string
	if !p.IsNew {
		pg, err := m.store.GetPage(r.Context(), id)
		if errors.Is(err, store.ErrNotFound) {
			m.notFound(w, r)
			return
		}
		if err != nil {
			m.serverError(w, r, err)
			return
		}
		p.Action = "/admin/pages/" + pg.ID
		p.DeleteURL = "/admin/pages/" + pg.ID + "/delete"
		p.Title, p.Alias, p.Body, p.ShowLayout = pg.Title, pg.Alias, pg.Body, pg.ShowLayout
		p.URL = m.base() + "/page/" + pg.Alias
		selected = pg.CategoryIDs
	}
	cats, err := m.pageCategories(r, selected)
	if err != nil {
		m.serverError(w, r, err)
		return
	}
	p.Categories = cats

	data := m.newPageData(r, "Page Editor")
	data.Page = p
	render(w, "page.html", data)
}

// pageCategories is the multiselect's options with the page's own set
// ticked.
func (m *Module) pageCategories(r *http.Request, selected []string) ([]pageCategory, error) {
	cats, err := m.store.ListCategories(r.Context())
	if err != nil {
		return nil, err
	}
	on := make(map[string]bool, len(selected))
	for _, id := range selected {
		on[id] = true
	}
	out := make([]pageCategory, 0, len(cats))
	for _, c := range cats {
		out = append(out, pageCategory{ID: c.ID, Name: c.Name, Selected: on[c.ID]})
	}
	return out, nil
}

// pageSave is POST /admin/pages/{id} and /admin/pages/new. A refused
// save comes back as the form with what was typed and why (A21).
func (m *Module) pageSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	id := contentPathID(r)

	var existing *store.Page
	if id != "" {
		pg, err := m.store.GetPage(ctx, id)
		if errors.Is(err, store.ErrNotFound) {
			m.notFound(w, r)
			return
		}
		if err != nil {
			m.serverError(w, r, err)
			return
		}
		existing = pg
	}

	p := pageFormPage{
		IsNew:      existing == nil,
		Action:     "/admin/pages/new",
		Title:      strings.TrimSpace(r.PostFormValue("title")),
		Alias:      strings.TrimSpace(r.PostFormValue("alias")),
		Body:       r.PostFormValue("body"),
		ShowLayout: r.PostFormValue("showlayout") != "",
	}
	catIDs := r.PostForm["categories"]
	if existing != nil {
		p.Action = "/admin/pages/" + existing.ID
		p.DeleteURL = "/admin/pages/" + existing.ID + "/delete"
	}

	alias := p.Alias
	if alias == "" {
		// A blank alias is made from the title, as the as-is does.
		alias = renderpkg.MakeTitle(p.Title)
	}

	var errs []string
	switch {
	case p.Title == "":
		errs = append(errs, "The title cannot be blank.")
	case len([]rune(p.Title)) > maxPageTitle:
		errs = append(errs, "The title may be at most 255 characters.")
	}
	if strings.TrimSpace(p.Body) == "" {
		errs = append(errs, "The body cannot be blank.")
	}
	errs = append(errs, aliasErrors(alias, maxPageAlias)...)

	if len(errs) == 0 {
		pages, err := m.store.ListPages(ctx)
		if err != nil {
			m.serverError(w, r, err)
			return
		}
		for _, other := range pages {
			if existing != nil && other.ID == existing.ID {
				continue
			}
			if strings.EqualFold(other.Alias, alias) {
				errs = append(errs, "A page with the alias "+alias+" already exists.")
			}
		}
	}

	if len(errs) == 0 {
		pg := &store.Page{Title: p.Title, Alias: alias, Body: p.Body, ShowLayout: p.ShowLayout}
		var err error
		if existing != nil {
			pg.ID = existing.ID
			err = m.store.UpdatePage(ctx, pg)
		} else {
			err = m.store.CreatePage(ctx, pg)
		}
		switch {
		case errors.Is(err, store.ErrDuplicate):
			errs = append(errs, "A page with the alias "+alias+" already exists.")
		case err != nil:
			m.serverError(w, r, err)
			return
		default:
			if err := m.store.SetPageCategories(ctx, pg.ID, catIDs); err != nil {
				m.serverError(w, r, err)
				return
			}
			m.flush()
			m.reinit()
			http.Redirect(w, r, "/admin/pages?saved=1", http.StatusFound)
			return
		}
	}

	cats, err := m.pageCategories(r, catIDs)
	if err != nil {
		m.serverError(w, r, err)
		return
	}
	p.Categories = cats
	p.Alias = alias
	p.Errors = errs
	data := m.newPageData(r, "Page Editor")
	data.Page = p
	render(w, "page.html", data)
}

// pageDelete is POST /admin/pages/{id}/delete. The store takes the
// category links with the row.
func (m *Module) pageDelete(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		m.notFound(w, r)
		return
	}
	if err := m.store.DeletePage(r.Context(), id); err != nil {
		m.serverError(w, r, err)
		return
	}
	m.flush()
	m.reinit()
	http.Redirect(w, r, "/admin/pages?deleted=1", http.StatusFound)
}

// aliasErrors is the alias check the category editor does (categories.go),
// with the column's own width: a URL segment of letters, digits and
// hyphens that is not one of the paths the blog itself owns (PLAN §8).
func aliasErrors(alias string, max int) []string {
	switch {
	case alias == "":
		return []string{"The alias cannot be blank: the title has no letters or digits to make one from."}
	case len([]rune(alias)) > max:
		return []string{"The alias may be at most " + strconv.Itoa(max) + " characters."}
	case !aliasPattern.MatchString(alias):
		return []string{"The alias may only contain letters, numbers and hyphens."}
	case reservedAliases[strings.ToLower(alias)]:
		return []string{"The alias " + alias + " is a path the blog itself uses. Pick another."}
	}
	return nil
}

// contentPathID reads {id}, mapping `new` to the empty string as the
// entry and category screens do.
func contentPathID(r *http.Request) string {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "new" {
		return ""
	}
	return id
}
