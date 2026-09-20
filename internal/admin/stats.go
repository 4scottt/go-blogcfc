package admin

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/4scottt/go-blogcfc/internal/store"
)

// The stats screen (PLAN §9 A28, admin/stats.cfm and statsbyyear.cfm).
// The as-is drew seven jQuery tabs; these are seven plain tables under
// their own headings, so every section is readable without JavaScript
// and the walk can key on a heading. `/admin/stats/{year}` is the same
// seven reports bounded by that year in the blog's zone, which is what
// statsbyyear.cfm did with its own near-copies of the queries.

// statsTop is how many rows each top table shows (the as-is `top 10`).
const statsTop = 10

// statsSection is one table on the screen.
type statsSection struct {
	ID    string
	Title string
	Head  [2]string
	Rows  []statsRow
}

// statsRow is one line: a label, maybe a link, and a number.
type statsRow struct {
	Label string
	URL   string
	Value string
}

// statsPage is stats.html's own data.
type statsPage struct {
	Year     int
	Years    []int
	AllURL   string
	Sections []statsSection
}

// statsReport is GET /admin/stats and GET /admin/stats/{year}.
func (m *Module) statsReport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	loc := m.settings.Timezone()

	year := 0
	if raw := strings.TrimSpace(r.PathValue("year")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1000 || n > 9999 {
			m.notFound(w, r)
			return
		}
		year = n
	}
	var from, to time.Time
	if year > 0 {
		from = time.Date(year, 1, 1, 0, 0, 0, 0, loc).UTC()
		to = time.Date(year+1, 1, 1, 0, 0, 0, 0, loc).Add(-time.Second).UTC()
	}

	summary, err := m.store.StatsSummary(ctx, from, to)
	if err != nil {
		m.serverError(w, r, err)
		return
	}
	// "Entries in the last 30 days" is the as-is `last30` query, which
	// was always the last thirty days whatever else the page showed.
	last30, err := m.store.StatsSummary(ctx, time.Now().UTC().AddDate(0, 0, -30), time.Time{})
	if err != nil {
		m.serverError(w, r, err)
		return
	}
	topViews, err := m.store.TopEntriesByViewsBetween(ctx, from, to, statsTop)
	if err != nil {
		m.serverError(w, r, err)
		return
	}
	cats, err := m.store.CategoryEntryCounts(ctx, from, to)
	if err != nil {
		m.serverError(w, r, err)
		return
	}
	topComments, err := m.store.TopEntriesByComments(ctx, from, to, statsTop)
	if err != nil {
		m.serverError(w, r, err)
		return
	}
	catComments, err := m.store.TopCategoriesByComments(ctx, from, to, statsTop)
	if err != nil {
		m.serverError(w, r, err)
		return
	}
	terms, err := m.store.TopSearchTermsBetween(ctx, from, to, statsTop)
	if err != nil {
		m.serverError(w, r, err)
		return
	}
	commenters, err := m.store.TopCommenters(ctx, from, to, statsTop)
	if err != nil {
		m.serverError(w, r, err)
		return
	}
	years, err := m.store.EntryYears(ctx, loc)
	if err != nil {
		m.serverError(w, r, err)
		return
	}

	p := statsPage{Year: year, Years: years, AllURL: "/admin/stats"}
	general := statsSection{ID: "generalstats", Title: "General", Head: [2]string{"Measure", "Value"}}
	general.Rows = []statsRow{
		{Label: "Entries", Value: strconv.Itoa(summary.Entries)},
		{Label: "Entries in the last 30 days", Value: strconv.Itoa(last30.Entries)},
		{Label: "First entry", Value: dayOrDash(summary.FirstPosted, loc)},
		{Label: "Last entry", Value: dayOrDash(summary.LastPosted, loc)},
		{Label: "Comments", Value: strconv.Itoa(summary.Comments)},
		{Label: "Average comments per entry", Value: average(summary.Comments, summary.Entries)},
		{Label: "Views", Value: strconv.Itoa(summary.Views)},
		{Label: "Average views per entry", Value: average(summary.Views, summary.Entries)},
		{Label: "Verified subscribers", Value: strconv.Itoa(summary.Subscribers)},
	}
	p.Sections = append(p.Sections, general)

	p.Sections = append(p.Sections, entrySection("topviews", "Top Entries by Views", "Views", topViews))
	p.Sections = append(p.Sections, categorySection("categorystats", "Category Stats", "Entries", cats))
	p.Sections = append(p.Sections, entrySection("topentriesbycomments", "Top Entries by Comments", "Comments", topComments))
	p.Sections = append(p.Sections, categorySection("topcategoriesbycomments", "Top Categories by Comments", "Comments", catComments))

	searchTerms := statsSection{ID: "topsearchterms", Title: "Top Search Terms", Head: [2]string{"Term", "Searches"}}
	for _, t := range terms {
		searchTerms.Rows = append(searchTerms.Rows, statsRow{
			Label: t.Term,
			URL:   "/search?search=" + url.QueryEscape(t.Term),
			Value: strconv.Itoa(t.Count),
		})
	}
	p.Sections = append(p.Sections, searchTerms)

	people := statsSection{ID: "topcommenters", Title: "Top Commenters", Head: [2]string{"Commenter", "Comments"}}
	for _, c := range commenters {
		people.Rows = append(people.Rows, statsRow{Label: c.Name, Value: strconv.Itoa(c.Count)})
	}
	p.Sections = append(p.Sections, people)

	data := m.newPageData(r, "Stats")
	data.Page = p
	render(w, "stats.html", data)
}

// entrySection is one table of entries with a number each.
func entrySection(id, title, head string, rows []store.EntryCount) statsSection {
	s := statsSection{ID: id, Title: title, Head: [2]string{"Entry", head}}
	for _, e := range rows {
		s.Rows = append(s.Rows, statsRow{
			Label: e.Title,
			URL:   "/admin/entries/" + e.ID,
			Value: strconv.Itoa(e.Count),
		})
	}
	return s
}

// categorySection is one table of categories with a number each.
func categorySection(id, title, head string, rows []store.CategoryCount) statsSection {
	s := statsSection{ID: id, Title: title, Head: [2]string{"Category", head}}
	for _, c := range rows {
		s.Rows = append(s.Rows, statsRow{
			Label: c.Name,
			URL:   "/?mode=cat&catid=" + url.QueryEscape(c.ID),
			Value: strconv.Itoa(c.Count),
		})
	}
	return s
}

// dayOrDash formats a date in the blog's zone, or a dash when there is
// none (the as-is printed a non-breaking space).
func dayOrDash(t time.Time, loc *time.Location) string {
	if t.IsZero() {
		return "-"
	}
	return t.In(loc).Format(dateLayout)
}

// average is the as-is `numberFormat(x/y,"999.99")`, and an empty blog
// divides by zero nowhere.
func average(total, n int) string {
	if n <= 0 {
		return "0.00"
	}
	return strconv.FormatFloat(float64(total)/float64(n), 'f', 2, 64)
}
