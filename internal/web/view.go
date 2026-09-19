package web

import (
	"html/template"
	"net/http"
	"strconv"
	"time"

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
	Version    string

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
		Version:         Version,
	}
}

// entryViews renders a page of entries.
func (m *Module) entryViews(entries []store.Entry, single bool) []entryView {
	base := m.base()
	loc := m.loc()
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
			Body:      template.HTML(e.Body), //nolint:gosec // entry markup is the admin's own, as in BlogCFC
			HasMore:   e.MoreBody != "",
			MoreURL:   link + "#more",
			MoreText:  m.bundle.T("more"),
			// Comment counting arrives with M3; the link is already the
			// one the entry page will answer.
			CommentCount: 0,
			CommentURL:   link + "#comments",
			PostedDate:   posted.Format("January 2, 2006"),
			PostedTime:   posted.Format("3:04 PM"),
			Views:        e.Views,
		}
		if single {
			v.MoreBody = template.HTML(e.MoreBody) //nolint:gosec // as above
		}
		for _, c := range e.Categories {
			v.Categories = append(v.Categories, linkView{Label: c.Name, URL: CategoryURL(base, c)})
		}
		out = append(out, v)
	}
	return out
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
