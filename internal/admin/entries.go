package admin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
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
		m.flush()
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

	// Enclosure is the stored file's name and Filesize and Mimetype
	// what the entry page and the feed say about it (PLAN §9 A07).
	// All three travel through a refused save and through a preview in
	// hidden fields, as the as-is carried oldenclosure, oldfilesize and
	// oldmimetype (admin/entry.cfm).
	Enclosure       string
	Filesize        int64
	Mimetype        string
	ManualEnclosure string
	// DownloadURL is the enclosure's public link. The as-is said it
	// "won't show up until you save the entry": there is no id to build
	// it from before that.
	DownloadURL string

	// Related is the chosen related entries, newest first; the picker's
	// multiselect holds them and submits their ids (PLAN §9 A09).
	Related []entryRef
	// ProxyURL is where the picker's filter fetches its JSON.
	ProxyURL string

	// Comments is the entry's thread, held comments included, each row
	// linking to the comment editor (the editor's fourth tab).
	Comments []entryCommentRow
}

// entryRef is one entry as the related picker shows it, and as
// /admin/proxy hands it over: the JSON field names are the contract the
// editor's script reads (PLAN §9 A09).
type entryRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// entryCommentRow is one line of the editor's Comments tab.
type entryCommentRow struct {
	ID        string
	Name      string
	Email     string
	Posted    string
	Excerpt   string
	Moderated bool
	URL       string
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
		ProxyURL:       adminProxyPath,
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
		p.Enclosure = e.Enclosure
		p.Filesize = e.FileSize
		p.Mimetype = e.MimeType
		p.DownloadURL = entryDownloadURL(e.ID, e.Enclosure)
		p.ViewURL = "/?mode=entry&entry=" + url.QueryEscape(e.ID) + "&adminview=1"
		for _, c := range e.Categories {
			p.Selected[c.ID] = true
		}
		related, err := m.store.RelatedEntriesForEditor(r.Context(), e.ID)
		if err != nil {
			m.serverError(w, r, err)
			return
		}
		for _, rel := range related {
			p.Related = append(p.Related, entryRef{ID: rel.ID, Title: rel.Title})
		}
		if p.Comments, err = m.entryComments(r.Context(), e.ID); err != nil {
			m.serverError(w, r, err)
			return
		}
	}

	data := m.newPageData(r, "Entry Editor")
	data.Page = p
	render(w, "entry.html", data)
}

// entrySave is POST /admin/entries/{id} and /admin/entries/new: the
// editor's one action. `preview` renders the entry without saving it
// (PLAN §9 A10), `return` comes back from that preview with everything
// still in the form, and anything else saves: validate, split the body,
// attach the categories and the related entries, then back to the list
// (PLAN §9 A05-A09).
func (m *Module) entrySave(w http.ResponseWriter, r *http.Request) {
	if err := parseEntryForm(r); err != nil {
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
	}
	p.ProxyURL = adminProxyPath
	p.Action = "/admin/entries/new"
	if existing != nil {
		p.Action = "/admin/entries/" + existing.ID
		p.ViewURL = "/?mode=entry&entry=" + url.QueryEscape(existing.ID) + "&adminview=1"
		p.Enclosure = existing.Enclosure
		p.Filesize = existing.FileSize
		p.Mimetype = existing.MimeType
		if p.Comments, err = m.entryComments(ctx, existing.ID); err != nil {
			m.serverError(w, r, err)
			return
		}
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

	// The enclosure the form carries wins over the stored one: it is
	// what a refused save or a preview handed back (admin/entry.cfm's
	// oldenclosure). Then the delete, the upload and the manual name are
	// applied in the as-is order, before anything can refuse the save,
	// so an uploaded file survives a validation error and a preview.
	if _, ok := r.PostForm["oldenclosure"]; ok {
		p.Enclosure = enclosureFileName(r.PostFormValue("oldenclosure"))
		p.Filesize, _ = strconv.ParseInt(strings.TrimSpace(r.PostFormValue("oldfilesize")), 10, 64)
		p.Mimetype = clip(strings.TrimSpace(r.PostFormValue("oldmimetype")), 100)
	}
	encErrs := m.applyEnclosure(r, &p)
	if p.Filesize < 0 {
		p.Filesize = 0
	}
	if existing != nil {
		p.DownloadURL = entryDownloadURL(existing.ID, p.Enclosure)
	}

	// The related picker's ids, in the order they were submitted. An id
	// that is not an entry any more simply drops out, which is how the
	// as-is behaved when it looked each one up for its title.
	relatedIDs, related, err := m.relatedFromForm(ctx, r.PostForm["related"], id)
	if err != nil {
		m.serverError(w, r, err)
		return
	}
	p.Related = related

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

	// Preview and Return, the two buttons beside Save. Neither writes
	// anything (PLAN §9 A10).
	if r.PostFormValue("preview") != "" {
		m.entryPreview(w, r, p)
		return
	}
	if r.PostFormValue("return") != "" && r.PostFormValue("save") == "" {
		data := m.newPageData(r, "Entry Editor")
		data.Page = p
		render(w, "entry.html", data)
		return
	}

	errs := encErrs
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

	// The release hook is told what the stored flag was before this save,
	// so it can tell a first release from a re-save. Read it now: e and
	// existing are the same pointer below.
	releasedBefore := existing != nil && existing.Released

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
	e.Enclosure = p.Enclosure
	e.FileSize = p.Filesize
	e.MimeType = p.Mimetype

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
	// Related entries: delete then insert, the set the picker submitted
	// (PLAN §9 A09, store.SetRelatedEntries).
	if err := m.store.SetRelatedEntries(ctx, e.ID, relatedIDs); err != nil {
		m.serverError(w, r, err)
		return
	}
	// Release side effects (subscriber mail, pings) run once the entry is
	// whole, categories included (PLAN §11). They are best effort: the
	// entry is saved either way, and a failed mail must not tell the
	// author their save did not happen.
	if err := m.release(ctx, e, releasedBefore); err != nil {
		slog.Error("admin: release hook", "entry", e.ID, "released_before", releasedBefore, "error", err)
	}
	// Every write drops the caches: the home page, the feeds and the
	// pods are all downstream of this entry (PLAN §11, §9 A29).
	m.flush()
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

// Enclosures (PLAN §9 A07, §6 "DATA_DIR"). An entry's enclosure is one
// file under `{DATA_DIR}/enclosures/`, served at `/enclosures/{file}`
// and logged through `/download/{id}/{file}` (internal/web/download.go).
// The editor stores the name only; the as-is stored an absolute server
// path in the same column and had to take its last segment everywhere
// it used one.

// maxEnclosureMemory is how much of a multipart body is held in memory
// before the rest spills to a temporary file.
const maxEnclosureMemory = 32 << 20

// maxEnclosureBytes caps one stored enclosure at 512 MiB, which is well
// past a podcast episode and short of filling a disk by accident.
const maxEnclosureBytes = 512 << 20

// enclosureNameMax is the `enclosure` column's width, in characters.
const enclosureNameMax = 255

// enclosureTypes is the small mime table the as-is borrowed from
// coldfusionmuse's mime.types for a manually named enclosure. Go's own
// table is built from /etc/mime.types, which a distroless image does not
// have, so the types that matter for a blog are named here.
var enclosureTypes = map[string]string{
	".mp3": "audio/mpeg", ".m4a": "audio/mp4", ".m4v": "video/mp4",
	".mp4": "video/mp4", ".ogg": "audio/ogg", ".oga": "audio/ogg",
	".wav": "audio/wav", ".aac": "audio/aac", ".flac": "audio/flac",
	".mov": "video/quicktime", ".webm": "video/webm",
	".pdf": "application/pdf", ".zip": "application/zip",
	".gz": "application/gzip", ".txt": "text/plain", ".xml": "application/xml",
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".gif": "image/gif", ".webp": "image/webp", ".svg": "image/svg+xml",
}

// parseEntryForm reads the editor's POST. The form is multipart because
// of the enclosure field, but a plain urlencoded post (a test, a script,
// the Pilot) still works.
func parseEntryForm(r *http.Request) error {
	if ct := r.Header.Get("Content-Type"); ct != "" {
		if mt, _, err := mime.ParseMediaType(ct); err == nil && strings.HasPrefix(mt, "multipart/") {
			return r.ParseMultipartForm(maxEnclosureMemory)
		}
	}
	return r.ParseForm()
}

// applyEnclosure runs the three things the editor can do to an
// enclosure, in the as-is order: delete the current one, store an
// upload, or adopt a file already in the folder by name. It returns the
// messages the form should show; anything it changes is in p.
func (m *Module) applyEnclosure(r *http.Request, p *entryFormPage) []string {
	var errs []string
	dir := m.enclosureDir()

	if r.PostFormValue("deleteenclosure") != "" {
		if p.Enclosure != "" {
			if err := os.Remove(filepath.Join(dir, p.Enclosure)); err != nil && !errors.Is(err, fs.ErrNotExist) {
				slog.Warn("admin: delete enclosure", "file", p.Enclosure, "error", err)
			}
		}
		p.Enclosure, p.Filesize, p.Mimetype = "", 0, ""
	}

	// An upload wins over a manual name, as the as-is cfif order did.
	if file, header, err := r.FormFile("enclosure"); err == nil {
		defer file.Close() //nolint:errcheck // read-only
		name, size, mimetype, err := storeEnclosure(dir, header.Filename, file)
		if err != nil {
			slog.Error("admin: store enclosure", "name", header.Filename, "error", err)
			return append(errs, "The enclosure could not be stored.")
		}
		p.Enclosure, p.Filesize, p.Mimetype = name, size, mimetype
		return errs
	}

	if manual := enclosureFileName(r.PostFormValue("manualenclosure")); manual != "" {
		p.ManualEnclosure = manual
		info, err := os.Stat(filepath.Join(dir, manual))
		if err != nil || info.IsDir() {
			return append(errs, "There is no file named "+manual+" in the enclosures folder.")
		}
		p.Enclosure, p.Filesize, p.Mimetype = manual, info.Size(), enclosureMime(dir, manual)
		p.ManualEnclosure = ""
	}
	return errs
}

// enclosureDir is `{DATA_DIR}/enclosures`.
func (m *Module) enclosureDir() string {
	return filepath.Join(m.cfg.DataDir, "enclosures")
}

// storeEnclosure writes an upload under a name nobody else is using and
// returns that name, the bytes written and the type. The as-is asked
// cffile for `nameconflict="makeunique"`, which appends a number to the
// stem; the same shape is kept, with the file created exclusively so two
// uploads at once cannot land on one name.
func storeEnclosure(dir, filename string, src io.Reader) (string, int64, string, error) {
	name := enclosureFileName(filename)
	if name == "" {
		name = "enclosure"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", 0, "", fmt.Errorf("admin: enclosure folder: %w", err)
	}
	ext := path.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := range 1000 {
		candidate := name
		if i > 0 {
			candidate = clip(stem+strconv.Itoa(i), enclosureNameMax-len(ext)) + ext
		}
		f, err := os.OpenFile(filepath.Join(dir, candidate), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", 0, "", fmt.Errorf("admin: create enclosure: %w", err)
		}
		size, copyErr := io.Copy(f, io.LimitReader(src, maxEnclosureBytes))
		closeErr := f.Close()
		if err := errors.Join(copyErr, closeErr); err != nil {
			_ = os.Remove(filepath.Join(dir, candidate))
			return "", 0, "", fmt.Errorf("admin: write enclosure: %w", err)
		}
		return candidate, size, enclosureMime(dir, candidate), nil
	}
	return "", 0, "", errors.New("admin: no free name for the enclosure")
}

// enclosureMime is the type recorded with an enclosure: the extension
// when it is one a blog serves, Go's table next, and the file's own
// first bytes last. The feed's `<enclosure type>` and the audio player
// in a rendered entry both read this (PLAN §9 F04, R02).
func enclosureMime(dir, name string) string {
	ext := strings.ToLower(path.Ext(name))
	if t, ok := enclosureTypes[ext]; ok {
		return t
	}
	if t := mime.TypeByExtension(ext); t != "" {
		return clip(t, 100)
	}
	f, err := os.Open(filepath.Join(dir, name))
	if err != nil {
		return "application/octet-stream"
	}
	defer f.Close() //nolint:errcheck // read-only
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	if n == 0 {
		return "application/octet-stream"
	}
	return clip(http.DetectContentType(head[:n]), 100)
}

// enclosureFileName reduces whatever was typed or uploaded to a plain file
// name: no directories, no traversal, nothing the enclosures folder
// cannot hold. An empty result means there is no usable name.
func enclosureFileName(raw string) string {
	name := strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	name = strings.TrimSpace(path.Base(name))
	switch {
	case name == "", name == ".", name == "..", name == "/":
		return ""
	case strings.ContainsAny(name, "\x00"):
		return ""
	}
	return clip(name, enclosureNameMax)
}

// entryDownloadURL is the logged download link for an entry's
// enclosure, empty when there is no entry yet or no file.
func entryDownloadURL(id, enclosure string) string {
	name := enclosureFileName(enclosure)
	if id == "" || name == "" {
		return ""
	}
	return "/download/" + url.PathEscape(id) + "/" + url.PathEscape(name)
}

// relatedFromForm turns the picker's submitted ids into the set to save
// and the rows to show again. The entry never relates to itself and an
// id that is no longer an entry is dropped.
func (m *Module) relatedFromForm(ctx context.Context, ids []string, selfID string) ([]string, []entryRef, error) {
	var out []string
	var refs []entryRef
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || id == selfID || seen[id] {
			continue
		}
		seen[id] = true
		e, err := m.store.GetEntry(ctx, id)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		out = append(out, e.ID)
		refs = append(refs, entryRef{ID: e.ID, Title: e.Title})
	}
	return out, refs, nil
}

// entryComments is the editor's Comments tab: the thread as the admin
// sees it, held comments included, each linking to the comment editor
// (PLAN §9 A13's screen).
func (m *Module) entryComments(ctx context.Context, entryID string) ([]entryCommentRow, error) {
	comments, err := m.store.ListComments(ctx, entryID, true)
	if err != nil {
		return nil, err
	}
	loc := m.settings.Timezone()
	out := make([]entryCommentRow, 0, len(comments))
	for _, c := range comments {
		text := strings.TrimSpace(c.Comment)
		excerpt := clip(text, 100)
		if excerpt != text {
			excerpt += "..."
		}
		out = append(out, entryCommentRow{
			ID:        c.ID,
			Name:      c.Name,
			Email:     c.Email,
			Posted:    c.Posted.In(loc).Format(postedLayout),
			Excerpt:   excerpt,
			Moderated: c.Moderated,
			URL:       "/admin/comments/" + c.ID,
		})
	}
	return out, nil
}
