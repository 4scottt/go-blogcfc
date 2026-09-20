package web

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/4scottt/go-blogcfc/internal/store"
)

// listing is one list view's shape: the filter that selects its entries,
// the title the layout adds and whether an empty result is "no entries"
// or "no entries for your criteria" (PLAN §9 P16).
type listing struct {
	additionalTitle string
	filter          store.EntryFilter
	criteria        bool
}

// handleHome is `/`: the newest entries, or one of the two id-based views
// BlogCFC keeps on the home page for alias-less rows (PLAN §8). A mode
// with no id falls back to the home listing, as getmode.cfm does.
func (m *Module) handleHome(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	// The owner's one-click links out of a comment notification
	// (PLAN §8, §9 C11). BlogApplication.cfc ran both in onRequestStart,
	// before anything else and without asking who was calling: the kill
	// token is the credential, and the approve link is only ever mailed
	// to the owner. Both answer with a line saying what happened.
	if token := strings.TrimSpace(q.Get("killcomment")); token != "" {
		m.killComment(w, r, token)
		return
	}
	if id := strings.TrimSpace(q.Get("approvecomment")); id != "" {
		m.approveComment(w, r, id)
		return
	}
	switch strings.ToLower(strings.TrimSpace(q.Get("mode"))) {
	case "entry":
		if id := strings.TrimSpace(q.Get("entry")); id != "" {
			m.entryByID(w, r, id)
			return
		}
	case "cat":
		if ids := strings.TrimSpace(q.Get("catid")); ids != "" {
			m.categoryByIDs(w, r, ids)
			return
		}
	}
	m.renderListing(w, r, listing{})
}

// handleSES serves BlogCFC's SES date forms, which parseses.cfm reads off
// the path: /{year}/{month}, /{year}/{month}/{day} and the permalink
// /{year}/{month}/{day}/{alias}. Empty segments are dropped, as CFML's
// list functions do, so a trailing slash changes nothing.
func (m *Module) handleSES(w http.ResponseWriter, r *http.Request) {
	segments := pathSegments(r.URL.Path)
	switch len(segments) {
	case 1:
		// Only reachable for a path like "/coldfusion/" that the
		// single-segment route did not take.
		m.categoryByAlias(w, r, segments[0])
	case 2:
		if strings.EqualFold(segments[0], "postedby") {
			m.postedBy(w, r, segments[1])
			return
		}
		m.archive(w, r, segments[0], segments[1], "")
	case 3:
		m.archive(w, r, segments[0], segments[1], segments[2])
	case 4:
		m.entryByPath(w, r, segments[0], segments[1], segments[2], segments[3])
	default:
		m.notFound(w, r)
	}
}

// pathSegments splits a URL path and drops the empty elements, which is
// what CFML's listLen/listGetAt do for the same string.
func pathSegments(path string) []string {
	var out []string
	for _, s := range strings.Split(path, "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// archive serves the month and day archives. The bounds are read in the
// blog's zone, as BlogCFC's offset-shifted SQL did (PLAN §9 P07,
// §11 "Timezone").
func (m *Module) archive(w http.ResponseWriter, r *http.Request, rawYear, rawMonth, rawDay string) {
	year, ok := numericSegment(rawYear)
	if !ok {
		m.notFound(w, r)
		return
	}
	month, ok := numericSegment(rawMonth)
	if !ok || month < 1 || month > 12 {
		m.notFound(w, r)
		return
	}
	day := 0
	if rawDay != "" {
		day, ok = numericSegment(rawDay)
		if !ok || day < 1 || day > 31 {
			m.notFound(w, r)
			return
		}
	}
	from, to := monthRange(year, time.Month(month), day, m.loc())
	m.renderListing(w, r, listing{
		additionalTitle: "Archives",
		filter:          store.EntryFilter{From: &from, To: &to},
		criteria:        true,
	})
}

// entryByPath serves the SES permalink /{year}/{month}/{day}/{alias}.
// BlogCFC's parseses.cfm checks the year and the month and passes all
// three date segments to the query beside the alias, so an alias asked for
// under the wrong date finds nothing; here that is a 404 (PLAN §9 P04).
func (m *Module) entryByPath(w http.ResponseWriter, r *http.Request, rawYear, rawMonth, rawDay, alias string) {
	year, okYear := numericSegment(rawYear)
	month, okMonth := numericSegment(rawMonth)
	day, okDay := numericSegment(rawDay)
	alias = strings.TrimSpace(alias)
	if !okYear || !okMonth || month < 1 || month > 12 || !okDay || day < 1 || day > 31 || alias == "" {
		m.notFound(w, r)
		return
	}

	e, err := m.store.GetEntryByAlias(r.Context(), alias)
	if errors.Is(err, store.ErrNotFound) {
		m.notFound(w, r)
		return
	}
	if err != nil {
		m.serverError(w, r, err)
		return
	}
	posted := e.Posted.In(m.loc())
	if posted.Year() != year || int(posted.Month()) != month || posted.Day() != day {
		m.notFound(w, r)
		return
	}
	m.renderEntry(w, r, e)
}

// entryByID serves /?mode=entry&entry={id}.
func (m *Module) entryByID(w http.ResponseWriter, r *http.Request, id string) {
	e, err := m.store.GetEntry(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		m.notFound(w, r)
		return
	}
	if err != nil {
		m.serverError(w, r, err)
		return
	}
	m.renderEntry(w, r, e)
}

// categoryByIDs serves /?mode=cat&catid=a,b: BlogCFC's id fallback, which
// takes a comma list (PLAN §9 P06).
func (m *Module) categoryByIDs(w http.ResponseWriter, r *http.Request, raw string) {
	var ids []string
	var names []string
	for _, id := range strings.Split(raw, ",") {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		c, err := m.store.GetCategory(r.Context(), id)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			m.serverError(w, r, err)
			return
		}
		ids = append(ids, c.ID)
		names = append(names, c.Name)
	}
	if len(ids) == 0 {
		m.notFound(w, r)
		return
	}
	m.renderListing(w, r, listing{
		additionalTitle: strings.Join(names, ", "),
		filter:          store.EntryFilter{CategoryIDs: ids},
		criteria:        true,
	})
}

// handleCategoryByAlias serves /{alias}.
func (m *Module) handleCategoryByAlias(w http.ResponseWriter, r *http.Request) {
	m.categoryByAlias(w, r, r.PathValue("alias"))
}

// categoryByAlias renders a category listing. The segments later
// milestones own are reserved and never resolve as a category (PLAN §18).
func (m *Module) categoryByAlias(w http.ResponseWriter, r *http.Request, rawAlias string) {
	alias := strings.TrimSpace(rawAlias)
	if alias == "" || reservedSegments[strings.ToLower(alias)] {
		m.notFound(w, r)
		return
	}
	c, err := m.store.GetCategoryByAlias(r.Context(), alias)
	if errors.Is(err, store.ErrNotFound) {
		m.notFound(w, r)
		return
	}
	if err != nil {
		m.serverError(w, r, err)
		return
	}
	m.renderListing(w, r, listing{
		additionalTitle: c.Name,
		filter:          store.EntryFilter{CategoryIDs: []string{c.ID}},
		criteria:        true,
	})
}

// handlePostedBy serves /postedby/{username} (PLAN §9 P08).
func (m *Module) handlePostedBy(w http.ResponseWriter, r *http.Request) {
	m.postedBy(w, r, r.PathValue("username"))
}

// postedBy renders one author's entries. An unknown author is a 404, not
// an empty listing.
func (m *Module) postedBy(w http.ResponseWriter, r *http.Request, rawUsername string) {
	username := strings.TrimSpace(rawUsername)
	if username == "" {
		m.notFound(w, r)
		return
	}
	u, err := m.store.GetUser(r.Context(), username)
	if errors.Is(err, store.ErrNotFound) {
		m.notFound(w, r)
		return
	}
	if err != nil {
		m.serverError(w, r, err)
		return
	}
	title := u.Name
	if title == "" {
		title = u.Username
	}
	m.renderListing(w, r, listing{
		additionalTitle: m.bundle.T("postedby") + " " + title,
		filter:          store.EntryFilter{Username: u.Username},
		criteria:        true,
	})
}

// renderListing runs a list view: the live filter unless ?adminview=1 is
// honoured, the newest first, one page of `maxentries` from startRow.
func (m *Module) renderListing(w http.ResponseWriter, r *http.Request, l listing) {
	max := m.settings.MaxEntries()
	if max < 1 {
		max = 10
	}
	first := startRow(r)

	f := l.filter
	f.Sort = "posted"
	f.Desc = true
	// BlogCFC drops the released-and-not-future filter only for a
	// logged-in admin asking for it, so drafts and scheduled entries are
	// hidden from everyone else (PLAN §11, getmode.cfm).
	f.LiveOnly = !m.adminView(r)
	f.Offset = first - 1
	f.Limit = max

	entries, total, err := m.store.ListEntries(r.Context(), f)
	if err != nil {
		m.serverError(w, r, err)
		return
	}

	data := m.newPage(r, l.additionalTitle)
	data.Entries = m.entryViewsContext(r.Context(), entries, false)
	if len(entries) == 0 {
		data.EmptyHeading = m.bundle.T("sorry")
		if l.criteria {
			data.EmptyMessage = m.bundle.T("noentriesforcriteria")
		} else {
			data.EmptyMessage = m.bundle.T("noentries")
		}
	}
	data.Prev, data.Next = m.pager(r, first, max, total)
	m.render(w, "entries.html", http.StatusOK, data)
}

// renderEntry is the one-entry view: body and morebody, no [more] link,
// the related entries and the comments under it, and one view counted
// for a visitor who has not been here before (PLAN §9 P12-P15).
// A draft or a future entry is invisible unless an admin asked for
// ?adminview=1 (PLAN §11 "Three entry states").
//
// The view is counted after the entry is read, so the footer prints the
// count this visit found, as the as-is did: index.cfm had its query in
// hand before it called logView.
func (m *Module) renderEntry(w http.ResponseWriter, r *http.Request, e *store.Entry) {
	if !e.Live(time.Now()) && !m.adminView(r) {
		m.notFound(w, r)
		return
	}
	ctx := r.Context()
	data := m.newPage(r, e.Title)
	data.Single = true
	data.Entries = m.entryViewsContext(ctx, []store.Entry{*e}, true)
	if len(data.Entries) == 1 {
		m.decorateEntry(ctx, &data.Entries[0], e)
	}
	m.countView(w, r, e.ID)
	m.render(w, "entries.html", http.StatusOK, data)
}

// decorateEntry hangs the single-entry view's own blocks off the rendered
// entry: the related entries (P13), the comment list and its heading
// (P14), and either the add-comment link or the "comments not allowed"
// string (P15). BlogCFC counted comments twice on this page -- once for
// the header anchor, once for the list -- and so does this, through
// CountComments and ListComments; both hide unmoderated rows.
func (m *Module) decorateEntry(ctx context.Context, v *entryView, e *store.Entry) {
	v.RelatedHeader = m.bundle.T("relatedblogentries")
	v.Related = m.relatedViews(ctx, e.ID)
	v.Comments = m.commentViews(ctx, e.ID, v.URL)

	count, err := m.store.CountComments(ctx, e.ID)
	if err != nil {
		slog.Error("web: comment count failed", "entry", e.ID, "error", err)
		count = len(v.Comments)
	}
	v.CommentCount = count
	v.CommentHeader = fmt.Sprintf("%s (%d)", m.bundle.T("comments"), count)

	if e.AllowComments {
		v.AddCommentLabel = m.bundle.T("addcomment")
		v.AddCommentURL = m.base() + "/comments/add/" + url.PathEscape(e.ID)
		v.SubscribeLabel = m.bundle.T("addsub")
		v.SubscribeURL = m.base() + "/comments/subscribe/" + url.PathEscape(e.ID)
		return
	}
	v.CommentsNotAllowed = m.bundle.T("commentsnotallowed")
}

// paragraphFormat2 is BlogCFC's ParagraphFormat2 (org/camden/blog/
// utils.cfc, Ben Forta's UDF): line endings normalised, a tab widened to
// three non-breaking spaces, and every newline turned into a `<br />`.
// It is not the entry body's paragraph formatter -- that one, which
// starts a new `<p>` on a blank line, lives in the render package -- and
// a comment's single newlines are line breaks, not paragraph breaks.
func paragraphFormat2(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ReplaceAll(s, "\t", "&nbsp;&nbsp;&nbsp;")
	return strings.ReplaceAll(s, "\n", "<br />")
}

// linkPattern is the http(s) half of utils.cfc's replaceLinks pattern.
// BlogCFC also linked `ftp:`, `gopher:` and bare `www.`/`ftp.` prefixes;
// those are left as text here (PLAN §9 P14).
var linkPattern = regexp.MustCompile(`https?://[-\w?%,./&#!;@:=+~]*[A-Za-z0-9/]`)

// linkMax is replaceLinks' own `linkmax`: a URL longer than this is
// shown truncated, though the link still points at the whole of it.
const linkMax = 50

// replaceLinks escapes a comment and turns its URLs into anchors. The
// escaping is this package's, not BlogCFC's: index.cfm printed a stored
// comment raw. Escaping happens between the matches and over the URLs
// themselves, so nothing a commenter wrote reaches the page as markup.
func replaceLinks(s string) string {
	var b strings.Builder
	last := 0
	for _, loc := range linkPattern.FindAllStringIndex(s, -1) {
		b.WriteString(html.EscapeString(s[last:loc[0]]))
		link := s[loc[0]:loc[1]]
		label := link
		if len(label) >= linkMax {
			label = label[:linkMax] + "..."
		}
		b.WriteString(`<a href="` + html.EscapeString(link) + `" rel="nofollow noopener" target="_blank">`)
		b.WriteString(html.EscapeString(label) + `</a>`)
		last = loc[1]
	}
	b.WriteString(html.EscapeString(s[last:]))
	return b.String()
}

// numericSegment parses a date segment the way BlogCFC's getmode.cfm does:
// digits only, no sign, no padding rules, and `val(x) is x` in CFML means
// a stray "07" still parses. Anything else means the URL is not an SES one.
func numericSegment(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}
