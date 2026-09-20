package admin

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// The downloads report (PLAN §9 A27, admin/downloads.cfm). The as-is
// built its range out of six selects (start month/day/year and the
// same again for the end) and reported totals per entry; this takes two
// date fields and lists the fetches themselves, which is what the table
// the plan asks for wants and what the stored rows actually carry -
// the as-is even said in a comment that it never split the online
// plays from the downloads, and the `online` column here does.

// downloadsDefaultDays is the range the screen opens on.
const downloadsDefaultDays = 30

// dateLayout is how the report's two date fields read and write a day.
const dateLayout = "2006-01-02"

// downloadRow is one logged fetch, already formatted.
type downloadRow struct {
	When   string
	Entry  string
	Link   string
	File   string
	IP     string
	Online bool
}

// downloadsPage is downloads.html's own data.
type downloadsPage struct {
	From  string
	To    string
	Rows  []downloadRow
	Total int
}

// downloadsReport is GET /admin/downloads?from=&to=.
func (m *Module) downloadsReport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	loc := m.settings.Timezone()
	q := r.URL.Query()

	today := time.Now().In(loc)
	fromDay := today.AddDate(0, 0, -downloadsDefaultDays)
	if d, ok := parseDay(q.Get("from"), loc); ok {
		fromDay = d
	}
	toDay := today
	if d, ok := parseDay(q.Get("to"), loc); ok {
		toDay = d
	}
	// The bounds are whole days in the blog's zone: from the start of
	// the first to the last moment of the last.
	from := time.Date(fromDay.Year(), fromDay.Month(), fromDay.Day(), 0, 0, 0, 0, loc).UTC()
	to := time.Date(toDay.Year(), toDay.Month(), toDay.Day(), 23, 59, 59, 0, loc).UTC()

	list, err := m.store.ListDownloads(ctx, from, to)
	if err != nil {
		m.serverError(w, r, err)
		return
	}

	titles := map[string]string{}
	p := downloadsPage{
		From:  fromDay.Format(dateLayout),
		To:    toDay.Format(dateLayout),
		Total: len(list),
	}
	for _, d := range list {
		title, ok := titles[d.EntryID]
		if !ok {
			title = m.entryTitle(ctx, d.EntryID)
			titles[d.EntryID] = title
		}
		p.Rows = append(p.Rows, downloadRow{
			When:   d.DownloadedAt.In(loc).Format(postedLayout),
			Entry:  title,
			Link:   "/admin/entries/" + d.EntryID,
			File:   d.Enclosure,
			IP:     d.IP,
			Online: d.Online,
		})
	}

	data := m.newPageData(r, "Downloads")
	data.Page = p
	render(w, "downloads.html", data)
}

// entryTitle is an entry's title for a report row, or a placeholder for
// a row whose entry has since been deleted.
func (m *Module) entryTitle(ctx context.Context, id string) string {
	e, err := m.store.GetEntry(ctx, id)
	if err != nil {
		return "(deleted entry)"
	}
	return e.Title
}

// parseDay reads a `YYYY-MM-DD` field in the blog's zone.
func parseDay(raw string, loc *time.Location) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation(dateLayout, raw, loc)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}
