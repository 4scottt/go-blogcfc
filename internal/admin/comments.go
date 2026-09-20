package admin

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/4scottt/go-blogcfc/internal/store"
)

// commentsPerPage is the datatable's page size, which the as-is tag
// defaulted to 20 (client/tags/datatable.cfm) and PLAN §12 keeps.
const commentsPerPage = 20

// commentExcerpt is how much of a comment the list shows before the link
// to the editor: the as-is datacol took `left=100`.
const commentExcerpt = 100

// emailPattern is BlogCFC's isEmail (org/camden/blog/utils.cfc) with its
// 2007 TLD list replaced by "two letters or more", as the public comment
// form's copy of it is.
var emailPattern = regexp.MustCompile(`^['_a-z0-9-]+(\.['_a-z0-9-]+)*(\+['_a-z0-9-]+)*@[a-z0-9-]+(\.[a-z0-9-]+)*\.[a-z]{2,}$`)

// validEmail keeps the as-is length bounds: 64 characters of local part,
// 255 of domain.
func validEmail(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	local, domain, ok := strings.Cut(s, "@")
	if !ok || len(local) > 64 || len(domain) > 255 {
		return false
	}
	return emailPattern.MatchString(s)
}

// validWebsite is isURL: an http(s) address with a host. The as-is
// pattern allowed any scheme-less string that looked like a URL; a link
// the admin page will print wants a scheme.
func validWebsite(s string) bool {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// commentRow is one line of the comments table, already formatted.
type commentRow struct {
	ID         string
	Name       string
	Email      string
	Excerpt    string
	Posted     string
	Moderated  bool
	EntryTitle string
	// EntryURL is the public entry with `adminview`, anchored at this
	// comment, which is the as-is View column's link.
	EntryURL string
}

// commentsPage is comments.html's own data.
type commentsPage struct {
	Rows     []commentRow
	Search   string
	Total    int
	Page     int
	Pages    int
	PrevLink string
	NextLink string
}

// commentsList is GET /admin/comments: the searchable, paged table with
// its bulk-delete form (PLAN §9 A13, admin/comments.cfm).
func (m *Module) commentsList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	// `search` is the as-is field name; `q` is accepted because PLAN §8
	// names it, and a Pilot may have typed either.
	search := strings.TrimSpace(q.Get("search"))
	if search == "" {
		search = strings.TrimSpace(q.Get("q"))
	}
	page := 1
	if n, err := strconv.Atoi(q.Get("page")); err == nil && n > 1 {
		page = n
	}

	rows, total, err := m.store.SearchComments(r.Context(), search, (page-1)*commentsPerPage, commentsPerPage)
	if err != nil {
		m.serverError(w, r, err)
		return
	}

	p := commentsPage{
		Rows:   m.commentRows(rows),
		Search: search,
		Total:  total,
		Page:   page,
		Pages:  (total + commentsPerPage - 1) / commentsPerPage,
	}
	if page > 1 {
		p.PrevLink = commentsLink(search, page-1)
	}
	if page < p.Pages {
		p.NextLink = commentsLink(search, page+1)
	}

	data := m.newPageData(r, "Comments")
	data.Flash = commentsFlash(q)
	data.Page = p
	render(w, "comments.html", data)
}

// commentRows formats what the two comment tables show.
func (m *Module) commentRows(rows []store.CommentWithEntry) []commentRow {
	loc := m.settings.Timezone()
	out := make([]commentRow, 0, len(rows))
	for _, c := range rows {
		out = append(out, commentRow{
			ID:    c.ID,
			Name:  c.Name,
			Email: c.Email,
			// c.Comment is the embedded store.Comment; its text is the
			// field of the same name inside it.
			Excerpt:    excerpt(c.Comment.Comment, commentExcerpt),
			Posted:     c.Posted.In(loc).Format(postedLayout),
			Moderated:  c.Moderated,
			EntryTitle: c.EntryTitle,
			EntryURL: "/?mode=entry&entry=" + url.QueryEscape(c.EntryID) +
				"&adminview=1#c" + url.QueryEscape(c.ID),
		})
	}
	return out
}

// excerpt cuts a comment to n characters for a table cell.
func excerpt(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// commentsLink builds a list URL that keeps the search.
func commentsLink(search string, page int) string {
	v := url.Values{}
	if search != "" {
		v.Set("search", search)
	}
	if page > 1 {
		v.Set("page", strconv.Itoa(page))
	}
	if len(v) == 0 {
		return "/admin/comments"
	}
	return "/admin/comments?" + v.Encode()
}

// commentsFlash turns the marker a redirect carries into the banner.
func commentsFlash(q url.Values) string {
	switch {
	case q.Has("saved"):
		return "Comment saved."
	case q.Has("approved"):
		return "Comment approved."
	}
	if n, err := strconv.Atoi(q.Get("deleted")); err == nil && n > 0 {
		if n == 1 {
			return "1 comment deleted."
		}
		return strconv.Itoa(n) + " comments deleted."
	}
	return ""
}

// commentsDelete is POST /admin/comments/delete: the marked rows go
// (PLAN §9 A13, A29).
func (m *Module) commentsDelete(w http.ResponseWriter, r *http.Request) {
	n, err := m.deleteMarked(r)
	if err != nil {
		if errors.Is(err, errBadForm) {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		m.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/comments?deleted="+strconv.Itoa(n), http.StatusFound)
}

// errBadForm is an unparseable request body, which is a 400 and not a 500.
var errBadForm = errors.New("admin: malformed form")

// deleteMarked removes the comments the `mark` checkboxes name and says
// how many went. Both comment screens post the same form.
func (m *Module) deleteMarked(r *http.Request) (int, error) {
	if err := r.ParseForm(); err != nil {
		return 0, errBadForm
	}
	var ids []string
	for _, id := range r.PostForm["mark"] {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return 0, nil
	}
	if err := m.store.DeleteComments(r.Context(), ids); err != nil {
		return 0, err
	}
	m.flush()
	return len(ids), nil
}

// commentFormPage is comment.html's own data: the submitted values, so a
// refused save comes back with what was typed.
type commentFormPage struct {
	ID     string
	Action string
	Errors []string

	Name      string
	Email     string
	Website   string
	Comment   string
	Subscribe bool
	Moderated bool

	Posted     string
	EntryTitle string
	EntryURL   string
}

// commentForm is GET /admin/comments/{id} (PLAN §9 A13, admin/comment.cfm).
func (m *Module) commentForm(w http.ResponseWriter, r *http.Request) {
	c, ok := m.comment(w, r)
	if !ok {
		return
	}
	p := m.commentFormFor(r.Context(), c)
	p.Name, p.Email, p.Website = c.Name, c.Email, c.Website
	p.Comment, p.Subscribe, p.Moderated = c.Comment, c.Subscribe, c.Moderated

	data := m.newPageData(r, "Comment Editor")
	data.Page = p
	render(w, "comment.html", data)
}

// commentSave is POST /admin/comments/{id}. Save writes the fields;
// Approve moderates the stored comment and re-notifies the thread's
// subscribers, which is what the as-is button did by sending you to
// moderate.cfm?approve= (PLAN §9 A13, C12).
func (m *Module) commentSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	c, ok := m.comment(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	if r.PostForm.Has("approve") {
		// The as-is Approve was a plain button that navigated away, so
		// whatever was typed in the fields was discarded; it still is.
		if err := m.approveAndNotify(ctx, c); err != nil {
			m.serverError(w, r, err)
			return
		}
		http.Redirect(w, r, "/admin/comments?approved=1", http.StatusFound)
		return
	}

	p := m.commentFormFor(ctx, c)
	p.Name = clip(strings.TrimSpace(r.PostFormValue("name")), 50)
	p.Email = clip(strings.TrimSpace(r.PostFormValue("email")), 50)
	p.Website = clip(strings.TrimSpace(r.PostFormValue("website")), 255)
	p.Comment = strings.TrimSpace(r.PostFormValue("comment"))
	p.Subscribe = isYes(r.PostFormValue("subscribe"))
	p.Moderated = isYes(r.PostFormValue("moderated"))

	var errs []string
	if p.Name == "" {
		errs = append(errs, "The name cannot be blank.")
	}
	if p.Email == "" || !validEmail(p.Email) {
		errs = append(errs, "The email cannot be blank and must be a valid email address.")
	}
	if p.Website != "" && !validWebsite(p.Website) {
		errs = append(errs, "Website must be a valid URL.")
	}
	if p.Comment == "" {
		errs = append(errs, "The comment cannot be blank.")
	}
	if len(errs) > 0 {
		p.Errors = errs
		data := m.newPageData(r, "Comment Editor")
		data.Page = p
		render(w, "comment.html", data)
		return
	}

	c.Name, c.Email, c.Website = p.Name, p.Email, p.Website
	c.Comment, c.Subscribe, c.Moderated = p.Comment, p.Subscribe, p.Moderated
	if err := m.store.UpdateComment(ctx, c); err != nil {
		m.serverError(w, r, err)
		return
	}
	m.flush()
	http.Redirect(w, r, "/admin/comments?saved=1", http.StatusFound)
}

// comment loads the comment a route names, answering 404 when there is
// none. The as-is sent you back to the list instead, which hid typos.
func (m *Module) comment(w http.ResponseWriter, r *http.Request) (*store.Comment, bool) {
	id := strings.TrimSpace(r.PathValue("id"))
	c, err := m.store.GetComment(r.Context(), id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		m.notFound(w, r)
		return nil, false
	case err != nil:
		m.serverError(w, r, err)
		return nil, false
	}
	return c, true
}

// commentFormFor builds the parts of the editor that do not come from the
// form: the action, the timestamp and the entry this comment hangs off.
func (m *Module) commentFormFor(ctx context.Context, c *store.Comment) commentFormPage {
	p := commentFormPage{
		ID:     c.ID,
		Action: "/admin/comments/" + c.ID,
		Posted: c.Posted.In(m.settings.Timezone()).Format(postedLayout),
		EntryURL: "/?mode=entry&entry=" + url.QueryEscape(c.EntryID) +
			"&adminview=1#c" + url.QueryEscape(c.ID),
	}
	if e, err := m.store.GetEntry(ctx, c.EntryID); err == nil {
		p.EntryTitle = e.Title
	}
	return p
}

// approveAndNotify moderates a held comment and re-notifies the thread's
// subscribers (PLAN §9 C12). The notification is best effort: the
// approval has happened either way, so a mail that fails is logged, not
// a 500 on a screen that did its job.
func (m *Module) approveAndNotify(ctx context.Context, c *store.Comment) error {
	if err := m.store.ApproveComment(ctx, c.ID); err != nil {
		return err
	}
	c.Moderated = true
	m.flush()

	e, err := m.store.GetEntry(ctx, c.EntryID)
	if errors.Is(err, store.ErrNotFound) {
		// The entry went while the comment waited; there is nobody to tell.
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := m.notify(ctx, e, c, false); err != nil {
		slog.Error("admin: re-notify on approval", "comment", c.ID, "entry", e.ID, "error", err)
	}
	return nil
}

// isYes reads the yes/no selects the comment editor uses, as the as-is
// did with its `yes`/`no` option values. A ticked checkbox ("on") counts
// too, so the control can become one later without a behaviour change.
func isYes(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "yes", "on", "true", "1":
		return true
	}
	return false
}
