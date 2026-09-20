package web

import (
	"context"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/4scottt/go-blogcfc/internal/render"
	"github.com/4scottt/go-blogcfc/internal/store"
)

// pageData is what every template gets. The URLs are pre-built so a
// template never concatenates one and never sees the request's Host.
type pageData struct {
	BlogTitle       string
	AdditionalTitle string
	Description     string
	Keywords        string

	Base       string
	HomeURL    string
	SearchURL  string
	ContactURL string
	RSSURL     string
	CSSURL     string
	CodeCSSURL string
	Version    string

	// Sidebar is the rendered pods column, dropped into ul#sidebar by the
	// layout. It is empty until main.go gives Module.Sidebar a renderer
	// (the pods package); an empty value leaves the list empty, which is
	// what M1 and every test without pods want.
	Sidebar template.HTML

	// Body is the seam for a page whose whole content is rendered before
	// the template runs -- a static page, the print view, a search result
	// summary. The entry templates do not use it; the packages that add
	// those views set it and render it from their own "content" block.
	Body template.HTML

	// Entries is the listing; Single marks the one-entry view, where the
	// body is followed by morebody instead of a [more] link.
	Entries []entryView
	Single  bool

	// EmptyHeading and EmptyMessage carry BlogCFC's "Sorry" block when the
	// listing is empty (PLAN §9 P16).
	EmptyHeading string
	EmptyMessage string

	// Prev and Next are the pager (PLAN §9 P03); nil when there is no page
	// on that side.
	Prev *pagerLink
	Next *pagerLink
}

// pagerLink is one end of the pager.
type pagerLink struct {
	URL   string
	Label string
}

// entryView is one rendered entry.
type entryView struct {
	ID    string
	Title string
	URL   string

	// Month and Day are the date badge in the blog's zone.
	Month string
	Day   string

	Author     string
	AuthorURL  string
	Categories []linkView

	// Body and MoreBody are trusted HTML: BlogCFC stores entry markup and
	// renders it raw, and the admin editor is the only writer.
	Body     template.HTML
	MoreBody template.HTML
	HasMore  bool
	MoreURL  string
	MoreText string

	PostedDate   string
	PostedTime   string
	Views        int
	CommentCount int
	CommentURL   string

	// PrintURL is the entry's print view (PLAN §8 /print/{id}); the route
	// itself arrives with the entry-completion package.
	PrintURL string
	// DownloadURL is /download/{id}/{file} for an entry with an enclosure,
	// and empty for one without: the footer's Download link hangs off it
	// (PLAN §9 P11).
	DownloadURL string

	// The rest is the single-entry view only; a listing leaves it empty.

	// RelatedHeader and Related are the related-entries block under the
	// entry, absent when nothing is related (PLAN §9 P13).
	RelatedHeader string
	Related       []relatedView

	// CommentHeader is the small-caps heading over the list, Comments the
	// moderated comments themselves (PLAN §9 P14).
	CommentHeader string
	Comments      []commentView

	// AddCommentURL and AddCommentLabel are the link to the comment form,
	// SubscribeURL and SubscribeLabel the one beside it to the thread
	// subscription (index.cfm's second bracketed link, PLAN §9 C09), and
	// CommentsNotAllowed the string that stands in the pair's place when
	// the entry disallows comments (PLAN §9 P15). Either the two links
	// are set or the message is, never both.
	AddCommentURL      string
	AddCommentLabel    string
	SubscribeURL       string
	SubscribeLabel     string
	CommentsNotAllowed string
}

// relatedView is one line of the related-entries list: the title, its
// permalink and the localized date BlogCFC printed beside it.
type relatedView struct {
	Title  string
	URL    string
	Posted string
}

// commentView is one rendered comment (PLAN §9 P14, §12 "Comment list").
type commentView struct {
	// Anchor is the id the comment carries, "c" + the comment id, and
	// URL the permalink that lands on it.
	Anchor string
	URL    string
	// Number is the comment's 1-based place in the list, as the as-is
	// "#3" link showed it.
	Number int

	Name string
	// Website is the commenter's own URL, empty unless it is http(s):
	// anything else is dropped rather than linked.
	Website string
	// AvatarURL is the Gravatar, empty when `allowgravatars` is off.
	AvatarURL string

	// Said is the bundle's word between the name and the date, Posted the
	// localized "date at time" the line ends with.
	Said   string
	Posted string

	// Body is the comment escaped, linkified and line-broken: trusted
	// only because this package built every tag in it.
	Body template.HTML
	// Alt marks the even rows, which the stylesheet shades.
	Alt bool
}

// printData is the print view's whole document (PLAN §9 P18). It is not
// a pageData: print.cfm had no layout, no sidebar and no navigation.
type printData struct {
	BlogTitle  string
	Title      string
	CSSURL     string
	CodeCSSURL string

	// PostedAtLabel, Posted, PostedByLabel and Author are the byline;
	// CategoriesLabel and Categories the line under it.
	PostedAtLabel   string
	Posted          string
	PostedByLabel   string
	Author          string
	CategoriesLabel string
	Categories      []string

	Body     template.HTML
	MoreBody template.HTML
}

// linkView is a label with a URL.
type linkView struct {
	Label string
	URL   string
}

// newPage fills everything a page has regardless of its contents.
func (m *Module) newPage(r *http.Request, additionalTitle string) pageData {
	base := m.base()
	return pageData{
		BlogTitle:       m.settings.BlogTitle(),
		AdditionalTitle: additionalTitle,
		Description:     m.settings.BlogDescription(),
		Keywords:        m.settings.BlogKeywords(),
		Base:            base,
		HomeURL:         base + "/",
		SearchURL:       base + "/search",
		ContactURL:      base + "/contact",
		RSSURL:          base + "/rss",
		CSSURL:          base + "/static/css/site.css",
		CodeCSSURL:      base + "/static/css/code.css",
		Version:         Version,
		Sidebar:         m.sidebar(r),
	}
}

// sidebar renders the pods column, or nothing when no renderer is set.
func (m *Module) sidebar(r *http.Request) template.HTML {
	if m.Sidebar == nil {
		return ""
	}
	return m.Sidebar(r)
}

// entryViews renders a page of entries. It keeps the signature the rest
// of the site calls it with; entryViewsContext is the same work with the
// request's context, which the textblock lookup needs.
func (m *Module) entryViews(entries []store.Entry, single bool) []entryView {
	return m.entryViewsContext(context.Background(), entries, single)
}

// entryViewsContext renders a page of entries. Bodies go through
// render.Entry -- code blocks, enclosure decorations, textblocks and
// paragraphs, in BlogCFC's order (PLAN §9 R01-R03) -- and only the single
// entry view renders morebody, which the as-is rendered without the
// enclosure argument.
func (m *Module) entryViewsContext(ctx context.Context, entries []store.Entry, single bool) []entryView {
	base := m.base()
	loc := m.loc()
	if len(entries) == 0 {
		return nil
	}
	textblocks := m.textblocks(ctx)
	counts := m.commentCounts(ctx, entries)
	out := make([]entryView, 0, len(entries))
	for _, e := range entries {
		link := EntryURL(base, e, loc)
		posted := e.Posted.In(loc)
		v := entryView{
			ID:        e.ID,
			Title:     e.Title,
			URL:       link,
			Month:     posted.Format("Jan"),
			Day:       strconv.Itoa(posted.Day()),
			Author:    e.Username,
			AuthorURL: UserURL(base, e.Username),
			Body: render.Entry(e.Body, render.Options{
				Enclosure:    e.Enclosure,
				EnclosureURL: enclosureURL(base, e),
				MimeType:     e.MimeType,
				Textblocks:   textblocks,
			}),
			HasMore:      e.MoreBody != "",
			MoreURL:      link + "#more",
			MoreText:     m.bundle.T("more"),
			CommentCount: counts[e.ID],
			CommentURL:   link + "#comments",
			PostedDate:   posted.Format("January 2, 2006"),
			PostedTime:   posted.Format("3:04 PM"),
			Views:        e.Views,
			PrintURL:     base + "/print/" + url.PathEscape(e.ID),
			DownloadURL:  downloadURL(base, e),
		}
		if single && e.MoreBody != "" {
			v.MoreBody = render.Entry(e.MoreBody, render.Options{Textblocks: textblocks})
		}
		for _, c := range e.Categories {
			v.Categories = append(v.Categories, linkView{Label: c.Name, URL: CategoryURL(base, c)})
		}
		out = append(out, v)
	}
	return out
}

// commentCounts is the real comment count for a page of entries, in one
// query (PLAN §9 P10, P11). A failure counts nothing rather than failing
// the page: a missing number is better than no entries at all.
func (m *Module) commentCounts(ctx context.Context, entries []store.Entry) map[string]int {
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.ID)
	}
	counts, err := m.store.CountCommentsFor(ctx, ids)
	if err != nil {
		slog.Error("web: comment counts failed", "error", err)
		return map[string]int{}
	}
	return counts
}

// enclosureURL is where render.Entry points an image or audio enclosure:
// `{base}/enclosures/{file}`, the file served from DATA_DIR (PLAN §8).
// An entry without an enclosure has no URL, which render reads as "no
// decoration".
func enclosureURL(base string, e store.Entry) string {
	name := path.Base(filepath.ToSlash(strings.TrimSpace(e.Enclosure)))
	if e.Enclosure == "" || name == "." || name == "/" {
		return ""
	}
	return base + "/enclosures/" + url.PathEscape(name)
}

// downloadURL is the enclosure's download link, empty when the entry has
// no enclosure. BlogCFC stored a path and linked only its file name
// (PLAN §8 /download/{id}/{file}).
func downloadURL(base string, e store.Entry) string {
	name := path.Base(filepath.ToSlash(strings.TrimSpace(e.Enclosure)))
	if e.Enclosure == "" || name == "." || name == "/" {
		return ""
	}
	return base + "/download/" + url.PathEscape(e.ID) + "/" + url.PathEscape(name)
}

// pager builds BlogCFC's previous/next links. They keep the SES path and
// every query parameter but startRow, which is rewritten (PLAN §9 P03).
// BlogCFC's startRow is a 1-based row number, not a page number.
func (m *Module) pager(r *http.Request, startRow, max, total int) (prev, next *pagerLink) {
	if startRow > 1 {
		from := startRow - max
		if from < 1 {
			from = 1
		}
		prev = &pagerLink{URL: m.pageURL(r, from), Label: m.bundle.T("preventries")}
	}
	if total >= startRow+max {
		next = &pagerLink{URL: m.pageURL(r, startRow+max), Label: m.bundle.T("moreentries")}
	}
	return prev, next
}

// pageURL rewrites startRow in the current URL, keeping the path and the
// rest of the query. The host never comes from the request (PLAN §6).
func (m *Module) pageURL(r *http.Request, startRow int) string {
	q := r.URL.Query()
	q.Del("startRow")
	q.Del("startrow")
	if startRow > 1 {
		q.Set("startRow", strconv.Itoa(startRow))
	}
	u := m.base() + r.URL.EscapedPath()
	if encoded := q.Encode(); encoded != "" {
		u += "?" + encoded
	}
	return u
}

// startRow reads BlogCFC's pagination cursor. Anything that is not a
// positive whole number falls back to 1, as getmode.cfm does.
func startRow(r *http.Request) int {
	raw := r.URL.Query().Get("startRow")
	if raw == "" {
		raw = r.URL.Query().Get("startrow")
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 1
	}
	return n
}

// monthRange is the half-open [from, to] bound of a month or a day in the
// blog's zone, ready for store.EntryFilter's inclusive From/To.
func monthRange(year int, month time.Month, day int, loc *time.Location) (time.Time, time.Time) {
	from := time.Date(year, month, 1, 0, 0, 0, 0, loc)
	to := from.AddDate(0, 1, 0)
	if day > 0 {
		from = time.Date(year, month, day, 0, 0, 0, 0, loc)
		to = from.AddDate(0, 0, 1)
	}
	return from.UTC(), to.Add(-time.Second).UTC()
}
