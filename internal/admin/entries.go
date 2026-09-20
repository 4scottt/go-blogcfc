package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/4scottt/go-blogcfc/internal/auth"
	// renderpkg, not render: this package already has a render helper for
	// its templates (templates.go).
	renderpkg "github.com/4scottt/go-blogcfc/internal/render"
	"github.com/4scottt/go-blogcfc/internal/store"
)

// moreTag splits an entry into the part every list shows and the part the
// permalink adds (PLAN §11 "The <more/> split"). BlogCFC finds it with
// findNoCase, so the match is case-insensitive here too.
const moreTag = "<more/>"

// postedLayout is how the editor shows and reads `posted`. The value is in
// the blog's zone; the store keeps UTC (PLAN §11 "Timezone").
const postedLayout = "2006-01-02 15:04"

// laterMilestone is the placeholder the tabs and fields that belong to a
// later package carry, so the screen is honest about what is not here yet.
const laterMilestone = "Available in a later milestone"

// entryColumnLabels are the sortable columns of the list, in the order the
// table shows them. The keys are the values `?sort=` takes, which are also
// store.EntryFilter's (PLAN §9 A03).
var entryColumnLabels = []struct{ Key, Label string }{
	{"title", "Title"},
	{"posted", "Posted"},
	{"username", "Author"},
	{"views", "Views"},
}

// entryRow is one line of the entries table, already formatted.
type entryRow struct {
	ID       string
	Title    string
	Posted   string
	Username string
	Views    int
	Released bool
	// ViewURL is the public page with `adminview`, so a draft is visible.
	ViewURL string
}

// entriesPage is entries.html's own data.
type entriesPage struct {
	Rows     []entryRow
	Keywords string
	Sort     string
	Dir      string
	Total    int
	Page     int
	Pages    int
	// SortLinks maps a column key to the href its header carries: the same
	// filter and page size, that column, the other direction.
	SortLinks map[string]string
	Columns   []struct{ Key, Label string }
	PrevLink  string
	NextLink  string
	// DraftsOnly says the list is narrowed because the user may not
	// release (PLAN §9 A04).
	DraftsOnly bool
}

// entriesList is GET /admin/entries: the filtered, sorted, paged table
// with its bulk-delete form (PLAN §9 A03, A04).
func (m *Module) entriesList(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	q := r.URL.Query()

	keywords := strings.TrimSpace(q.Get("keywords"))
	sortCol := entrySortColumn(q.Get("sort"))
	dir := "desc"
	if strings.EqualFold(q.Get("dir"), "asc") {
		dir = "asc"
	}
	page := 1
	if n, err := strconv.Atoi(q.Get("page")); err == nil && n > 1 {
		page = n
	}
	size := m.settings.MaxEntriesAdmin()
	if size < 1 {
		size = 20
	}

	f := store.EntryFilter{
		Keywords: keywords,
		Sort:     sortCol,
		Desc:     dir == "desc",
		Limit:    size,
		Offset:   (page - 1) * size,
	}
	// A user who may not release entries sees drafts only: the as-is sets
	// params.released = false for exactly that case (admin/entries.cfm).
	canRelease := auth.HasRole(u, auth.RoleReleaseEntries)
	if !canRelease {
		draft := false
		f.Released = &draft
	}

	entries, total, err := m.store.ListEntries(r.Context(), f)
	if err != nil {
		m.serverError(w, r, err)
		return
	}

	loc := m.settings.Timezone()
	rows := make([]entryRow, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, entryRow{
			ID:       e.ID,
			Title:    e.Title,
			Posted:   e.Posted.In(loc).Format(postedLayout),
			Username: e.Username,
			Views:    e.Views,
			Released: e.Released,
			ViewURL:  "/?mode=entry&entry=" + url.QueryEscape(e.ID) + "&adminview=1",
		})
	}

	pages := (total + size - 1) / size
	p := entriesPage{
		Rows:       rows,
		Keywords:   keywords,
		Sort:       sortCol,
		Dir:        dir,
		Total:      total,
		Page:       page,
		Pages:      pages,
		SortLinks:  map[string]string{},
		Columns:    entryColumnLabels,
		DraftsOnly: !canRelease,
	}
	for _, c := range entryColumnLabels {
		// Clicking the column already sorted flips the direction; any
		// other column starts ascending.
		next := "asc"
		if sortCol == c.Key && dir == "asc" {
			next = "desc"
		}
		p.SortLinks[c.Key] = entriesLink(keywords, c.Key, next, 1)
	}
	if page > 1 {
		p.PrevLink = entriesLink(keywords, sortCol, dir, page-1)
	}
	if page < pages {
		p.NextLink = entriesLink(keywords, sortCol, dir, page+1)
	}

	data := m.newPageData(r, "Entries")
	data.Flash = entriesFlash(q)
	data.Page = p
	render(w, "entries.html", data)
}

// entriesFlash turns the marker a redirect carries into the banner the
// list shows once (pageData.Flash).
func entriesFlash(q url.Values) string {
	if q.Has("saved") {
		return "Entry saved."
	}
	if n, err := strconv.Atoi(q.Get("deleted")); err == nil && n > 0 {
		if n == 1 {
			return "1 entry deleted."
		}
		return strconv.Itoa(n) + " entries deleted."
	}
	return ""
}

// entrySortColumn keeps `?sort=` to the columns the store will order by.
func entrySortColumn(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "title":
		return "title"
	case "username":
		return "username"
	case "views":
		return "views"
	default:
		return "posted"
	}
}

// entriesLink builds a list URL that keeps the filter and the sort.
func entriesLink(keywords, sortCol, dir string, page int) string {
	v := url.Values{}
	if keywords != "" {
		v.Set("keywords", keywords)
	}
	v.Set("sort", sortCol)
	v.Set("dir", dir)
	if page > 1 {
		v.Set("page", strconv.Itoa(page))
	}
	return "/admin/entries?" + v.Encode()
}

// entriesDelete is POST /admin/entries/delete: the marked rows go, and the
// caches with them (PLAN §9 A03, A29).
func (m *Module) entriesDelete(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	u := auth.UserFrom(r.Context())
	canRelease := auth.HasRole(u, auth.RoleReleaseEntries)

	var ids []string
	for _, id := range r.PostForm["mark"] {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		// A user who only ever sees drafts may only delete drafts: the
		// list is already narrowed, and a hand-made POST should not get
		// past that. The as-is deletes whatever id it is handed.
		if !canRelease {
			e, err := m.store.GetEntry(r.Context(), id)
			if errors.Is(err, store.ErrNotFound) {
				continue
			}
			if err != nil {
				m.serverError(w, r, err)
				return
			}
			if e.Released {
				continue
			}
		}
		ids = append(ids, id)
	}
	if len(ids) > 0 {
		if err := m.store.DeleteEntries(r.Context(), ids); err != nil {
			m.serverError(w, r, err)
			return
		}
		m.reinit()
	}
	http.Redirect(w, r, "/admin/entries?deleted="+strconv.Itoa(len(ids)), http.StatusFound)
}

// entryFormPage is entry.html's own data: the submitted values, so a
// refused save comes back with what was typed, plus what the user may do.
type entryFormPage struct {
	IsNew  bool
	ID     string
	Action string
	Errors []string

	Title         string
	Body          string
	Alias         string
	Posted        string
	NewCategory   string
	Subtitle      string
	Keywords      string
	Summary       string
	Duration      string
	AllowComments bool
	SendEmail     bool
	Released      bool

	Categories     []store.Category
	Selected       map[string]bool
	CanRelease     bool
	CanAddCategory bool
	ViewURL        string
	LaterMilestone string
}

// entryForm is GET /admin/entries/{id} and /admin/entries/new (PLAN §9 A05).
func (m *Module) entryForm(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	id := entryPathID(r)

	cats, err := m.store.ListCategories(r.Context())
	if err != nil {
		m.serverError(w, r, err)
		return
	}

	p := entryFormPage{
		IsNew:          id == "",
		ID:             id,
		Categories:     cats,
		Selected:       map[string]bool{},
		CanRelease:     auth.HasRole(u, auth.RoleReleaseEntries),
		CanAddCategory: auth.HasRole(u, auth.RoleAddCategory),
		LaterMilestone: laterMilestone,
	}
	loc := m.settings.Timezone()

	if p.IsNew {
		p.Action = "/admin/entries/new"
		p.Posted = time.Now().In(loc).Format(postedLayout)
		p.AllowComments = true
		p.SendEmail = true
		// The as-is defaults released to whether you may release at all.
		p.Released = p.CanRelease
	} else {
		e, err := m.store.GetEntry(r.Context(), id)
		if errors.Is(err, store.ErrNotFound) {
			m.notFound(w, r)
			return
		}
		if err != nil {
			m.serverError(w, r, err)
			return
		}
		p.Action = "/admin/entries/" + e.ID
		p.Title = e.Title
		p.Body = joinMore(e.Body, e.MoreBody)
		p.Alias = e.Alias
		p.Posted = e.Posted.In(loc).Format(postedLayout)
		p.AllowComments = e.AllowComments
		p.SendEmail = e.SendEmail
		p.Released = e.Released
		p.Subtitle = e.Subtitle
		p.Keywords = e.Keywords
		p.Summary = e.Summary
		p.Duration = e.Duration
		p.ViewURL = "/?mode=entry&entry=" + url.QueryEscape(e.ID) + "&adminview=1"
		for _, c := range e.Categories {
			p.Selected[c.ID] = true
		}
	}

	data := m.newPageData(r, "Entry Editor")
	data.Page = p
	render(w, "entry.html", data)
}

// entrySave is POST /admin/entries/{id} and /admin/entries/new: validate,
// split the body, attach the categories, then back to the list
// (PLAN §9 A05, A06).
func (m *Module) entrySave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	u := auth.UserFrom(ctx)
	id := entryPathID(r)

	var existing *store.Entry
	if id != "" {
		e, err := m.store.GetEntry(ctx, id)
		if errors.Is(err, store.ErrNotFound) {
			m.notFound(w, r)
			return
		}
		if err != nil {
			m.serverError(w, r, err)
			return
		}
		existing = e
	}

	cats, err := m.store.ListCategories(ctx)
	if err != nil {
		m.serverError(w, r, err)
		return
	}

	p := entryFormPage{
		IsNew:          existing == nil,
		ID:             id,
		Categories:     cats,
		Selected:       map[string]bool{},
		CanRelease:     auth.HasRole(u, auth.RoleReleaseEntries),
		CanAddCategory: auth.HasRole(u, auth.RoleAddCategory),
		LaterMilestone: laterMilestone,
	}
	p.Action = "/admin/entries/new"
	if existing != nil {
		p.Action = "/admin/entries/" + existing.ID
		p.ViewURL = "/?mode=entry&entry=" + url.QueryEscape(existing.ID) + "&adminview=1"
	}

	p.Title = strings.TrimSpace(r.PostFormValue("title"))
	p.Body = strings.TrimSpace(r.PostFormValue("body"))
	p.Alias = strings.TrimSpace(r.PostFormValue("alias"))
	p.Posted = strings.TrimSpace(r.PostFormValue("posted"))
	p.NewCategory = strings.TrimSpace(r.PostFormValue("newcategory"))
	p.Subtitle = clip(strings.TrimSpace(r.PostFormValue("subtitle")), 100)
	p.Keywords = clip(strings.TrimSpace(r.PostFormValue("keywords")), 100)
	p.Summary = clip(strings.TrimSpace(r.PostFormValue("summary")), 255)
	p.Duration = clip(strings.TrimSpace(r.PostFormValue("duration")), 10)
	p.AllowComments = r.PostFormValue("allowcomments") != ""
	p.SendEmail = r.PostFormValue("sendemail") != ""

	// Released: only a ReleaseEntries user may change it. For everyone
	// else the stored flag stands, and a new entry is a draft.
	switch {
	case p.CanRelease:
		p.Released = r.PostFormValue("released") != ""
	case existing != nil:
		p.Released = existing.Released
	}

	// Categories: an id that is not a category any more is dropped, as the
	// as-is does by assigning only what it finds.
	known := make(map[string]bool, len(cats))
	for _, c := range cats {
		known[c.ID] = true
	}
	var catIDs []string
	for _, cid := range r.PostForm["categories"] {
		if known[cid] && !p.Selected[cid] {
			p.Selected[cid] = true
			catIDs = append(catIDs, cid)
		}
	}

	var errs []string
	switch {
	case p.Title == "":
		errs = append(errs, "You must include a title.")
	case len([]rune(p.Title)) > 100:
		errs = append(errs, "The title may be at most 100 characters.")
	}

	var body, more string
	switch idx := indexMore(p.Body); {
	case p.Body == "":
		errs = append(errs, "You must include a body.")
	case idx == 0:
		errs = append(errs, "An entry may not start with "+moreTag+": the part before it is what the lists show.")
	case idx > 0:
		body = strings.TrimSpace(p.Body[:idx])
		more = strings.TrimSpace(p.Body[idx+len(moreTag):])
	default:
		body = p.Body
	}

	posted, err := parsePosted(p.Posted, m.settings.Timezone())
	if err != nil {
		errs = append(errs, "The posted date must look like "+time.Now().Format(postedLayout)+".")
	}

	alias := p.Alias
	if alias == "" {
		alias = renderpkg.MakeTitle(p.Title)
	}
	if len([]rune(alias)) > 100 {
		errs = append(errs, "The alias may be at most 100 characters.")
	} else if alias != "" {
		other, err := m.store.GetEntryByAlias(ctx, alias)
		switch {
		case err == nil && (existing == nil || other.ID != existing.ID):
			errs = append(errs, "Another entry already uses the alias "+alias+".")
		case err != nil && !errors.Is(err, store.ErrNotFound):
			m.serverError(w, r, err)
			return
		}
	}

	if len(errs) > 0 {
		p.Errors = errs
		data := m.newPageData(r, "Entry Editor")
		data.Page = p
		render(w, "entry.html", data)
		return
	}

	// A new category is an AddCategory user's privilege; for anyone else
	// the field is not even rendered and its value is ignored.
	if p.NewCategory != "" && p.CanAddCategory {
		cid, err := m.categoryIDForName(ctx, cats, p.NewCategory)
		if err != nil {
			m.serverError(w, r, err)
			return
		}
		if !p.Selected[cid] {
			p.Selected[cid] = true
			catIDs = append(catIDs, cid)
		}
	}

	e := &store.Entry{Username: u.Username, AllowComments: p.AllowComments, SendEmail: p.SendEmail}
	if existing != nil {
		e = existing
	}
	// A draft released with a date already past is posted now, so it does
	// not appear half way down the home page (admin/entry.cfm, Shane
	// Zehnder's fix; PLAN §9 A06).
	if existing != nil && !existing.Released && p.Released && posted.Before(time.Now().UTC()) {
		posted = time.Now().UTC().Truncate(time.Second)
	}
	e.Title = p.Title
	e.Alias = alias
	e.Body = body
	e.MoreBody = more
	e.Posted = posted
	e.AllowComments = p.AllowComments
	e.SendEmail = p.SendEmail
	e.Released = p.Released
	e.Subtitle = p.Subtitle
	e.Keywords = p.Keywords
	e.Summary = p.Summary
	e.Duration = p.Duration

	if existing == nil {
		if err := m.store.CreateEntry(ctx, e); err != nil {
			m.serverError(w, r, err)
			return
		}
	} else if err := m.store.UpdateEntry(ctx, e); err != nil {
		m.serverError(w, r, err)
		return
	}
	if err := m.store.SetEntryCategories(ctx, e.ID, catIDs); err != nil {
		m.serverError(w, r, err)
		return
	}
	m.reinit()
	http.Redirect(w, r, "/admin/entries?saved=1", http.StatusFound)
}

// categoryIDForName returns the id of the category with this name,
// creating it when it is new. The as-is looks the name up first so a
// second entry with the same new category does not fail on the unique
// index (admin/entry.cfm).
func (m *Module) categoryIDForName(ctx context.Context, cats []store.Category, name string) (string, error) {
	name = clip(name, 50)
	for _, c := range cats {
		if strings.EqualFold(c.Name, name) {
			return c.ID, nil
		}
	}
	c := &store.Category{Name: name, Alias: clip(renderpkg.MakeTitle(name), 50)}
	if err := m.store.CreateCategory(ctx, c); err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			// Someone created it between the list and the insert, or the
			// alias collided; the row that is there wins.
			if found, gerr := m.store.GetCategoryByAlias(ctx, c.Alias); gerr == nil {
				return found.ID, nil
			}
		}
		return "", err
	}
	return c.ID, nil
}

// entryPathID reads {id} from the route, mapping the `new` spelling and
// the /new route alike to the empty string.
func entryPathID(r *http.Request) string {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "new" {
		return ""
	}
	return id
}

// indexMore finds the first <more/>, case-insensitively, or -1.
func indexMore(body string) int {
	return strings.Index(strings.ToLower(body), moreTag)
}

// joinMore puts an entry back together for the editor: the split is an
// editor concern, so the textarea shows what was typed (PLAN §11).
func joinMore(body, more string) string {
	if more == "" {
		return body
	}
	return body + moreTag + more
}

// parsePosted reads the editor's date in the blog's zone and returns UTC.
// An empty field is now, as a new entry's default already is.
func parsePosted(raw string, loc *time.Location) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Now().UTC().Truncate(time.Second), nil
	}
	for _, layout := range []string{postedLayout, "2006-01-02 15:04:05", "2006-01-02T15:04"} {
		if t, err := time.ParseInLocation(layout, raw, loc); err == nil {
			return t.UTC().Truncate(time.Second), nil
		}
	}
	return time.Time{}, fmt.Errorf("admin: posted %q is not %s", raw, postedLayout)
}

// clip cuts a value to the column's width in characters, not bytes.
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
