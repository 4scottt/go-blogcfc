package admin

import (
	"errors"
	"net/http"
	"regexp"
	"strings"

	renderpkg "github.com/4scottt/go-blogcfc/internal/render"
	"github.com/4scottt/go-blogcfc/internal/store"
)

// aliasPattern is what a category alias may contain. The as-is also allows
// spaces (`[^[:alnum:] -]`, admin/category.cfm); a space in a path segment
// is a nuisance the rewrite does without, so the alias is letters, digits
// and hyphens (PLAN §8: aliases are URL segments).
var aliasPattern = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// reservedAliases are the first path segments the public site owns, which
// therefore cannot be a category alias (PLAN §18). They are compared
// case-insensitively and without the leading slash.
var reservedAliases = map[string]bool{
	"search": true, "page": true, "print": true, "rss": true, "contact": true,
	"send": true, "comments": true, "download": true, "enclosures": true,
	"images": true, "slideshow": true, "postedby": true, "admin": true,
	"static": true, "health": true, "xmlrpc": true, "sitemap.xml": true,
	"robots.txt": true,
}

// categoriesPage is categories.html's own data.
type categoriesPage struct {
	Rows []store.Category
}

// categoriesList is GET /admin/categories (PLAN §9 A12).
func (m *Module) categoriesList(w http.ResponseWriter, r *http.Request) {
	cats, err := m.store.ListCategories(r.Context())
	if err != nil {
		m.serverError(w, r, err)
		return
	}
	data := m.newPageData(r, "Categories")
	data.Flash = categoriesFlash(r)
	data.Page = categoriesPage{Rows: cats}
	render(w, "categories.html", data)
}

// categoriesFlash is the banner a redirect asks for.
func categoriesFlash(r *http.Request) string {
	q := r.URL.Query()
	switch {
	case q.Has("saved"):
		return "Category saved."
	case q.Has("deleted"):
		return "Category deleted."
	default:
		return ""
	}
}

// categoryFormPage is category.html's own data.
type categoryFormPage struct {
	IsNew  bool
	ID     string
	Action string
	Errors []string
	Name   string
	Alias  string
}

// categoryForm is GET /admin/categories/{id} and /admin/categories/new.
func (m *Module) categoryForm(w http.ResponseWriter, r *http.Request) {
	id := categoryPathID(r)
	p := categoryFormPage{IsNew: id == "", ID: id, Action: "/admin/categories/new"}
	if !p.IsNew {
		c, err := m.store.GetCategory(r.Context(), id)
		if errors.Is(err, store.ErrNotFound) {
			m.notFound(w, r)
			return
		}
		if err != nil {
			m.serverError(w, r, err)
			return
		}
		p.Action = "/admin/categories/" + c.ID
		p.Name, p.Alias = c.Name, c.Alias
	}
	data := m.newPageData(r, "Category Editor")
	data.Page = p
	render(w, "category.html", data)
}

// categorySave is POST /admin/categories/{id} and /admin/categories/new.
// A refused save re-renders the form with what was typed and why it was
// refused (PLAN §9 A12).
func (m *Module) categorySave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	id := categoryPathID(r)

	var existing *store.Category
	if id != "" {
		c, err := m.store.GetCategory(ctx, id)
		if errors.Is(err, store.ErrNotFound) {
			m.notFound(w, r)
			return
		}
		if err != nil {
			m.serverError(w, r, err)
			return
		}
		existing = c
	}

	p := categoryFormPage{
		IsNew:  existing == nil,
		ID:     id,
		Action: "/admin/categories/new",
		Name:   strings.TrimSpace(r.PostFormValue("name")),
		Alias:  strings.TrimSpace(r.PostFormValue("alias")),
	}
	if existing != nil {
		p.Action = "/admin/categories/" + existing.ID
	}

	alias := p.Alias
	if alias == "" {
		// The as-is fills a blank alias from the name; so does the rewrite.
		alias = renderpkg.MakeTitle(p.Name)
	}

	var errs []string
	switch {
	case p.Name == "":
		errs = append(errs, "The name cannot be blank.")
	case len([]rune(p.Name)) > 50:
		errs = append(errs, "The name may be at most 50 characters.")
	}
	switch {
	case alias == "":
		errs = append(errs, "The alias cannot be blank: the name has no letters or digits to make one from.")
	case len([]rune(alias)) > 50:
		errs = append(errs, "The alias may be at most 50 characters.")
	case !aliasPattern.MatchString(alias):
		errs = append(errs, "The alias may only contain letters, numbers and hyphens.")
	case reservedAliases[strings.ToLower(alias)]:
		errs = append(errs, "The alias "+alias+" is a path the blog itself uses. Pick another.")
	}

	if len(errs) == 0 {
		// Name and alias are unique in the schema, but a check here says
		// which of the two clashed rather than "duplicate".
		cats, err := m.store.ListCategories(ctx)
		if err != nil {
			m.serverError(w, r, err)
			return
		}
		for _, c := range cats {
			if existing != nil && c.ID == existing.ID {
				continue
			}
			if strings.EqualFold(c.Name, p.Name) {
				errs = append(errs, "A category with this name already exists.")
			}
			if strings.EqualFold(c.Alias, alias) {
				errs = append(errs, "A category with the alias "+alias+" already exists.")
			}
		}
	}

	if len(errs) == 0 {
		c := &store.Category{Name: p.Name, Alias: alias}
		var err error
		if existing != nil {
			c.ID = existing.ID
			err = m.store.UpdateCategory(ctx, c)
		} else {
			err = m.store.CreateCategory(ctx, c)
		}
		switch {
		case errors.Is(err, store.ErrDuplicate):
			errs = append(errs, "A category with this name or alias already exists.")
		case err != nil:
			m.serverError(w, r, err)
			return
		default:
			m.flush()
			http.Redirect(w, r, "/admin/categories?saved=1", http.StatusFound)
			return
		}
	}

	p.Alias = alias
	p.Errors = errs
	data := m.newPageData(r, "Category Editor")
	data.Page = p
	render(w, "category.html", data)
}

// categoryDelete is POST /admin/categories/{id}/delete. The store purges
// the entry and page links with the row (PLAN §9 A12).
func (m *Module) categoryDelete(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		m.notFound(w, r)
		return
	}
	if err := m.store.DeleteCategory(r.Context(), id); err != nil {
		m.serverError(w, r, err)
		return
	}
	m.flush()
	http.Redirect(w, r, "/admin/categories?deleted=1", http.StatusFound)
}

// categoryPathID reads {id}, mapping the `new` spellings to the empty
// string as the entry screens do.
func categoryPathID(r *http.Request) string {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "new" {
		return ""
	}
	return id
}
