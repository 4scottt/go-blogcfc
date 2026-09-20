package pods

import (
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/4scottt/go-blogcfc/internal/i18n"
)

// The calendar pod: a month grid for the month the reader is looking at,
// with the days that have a live entry linked and today highlighted
// (PLAN §9 D01, client/includes/pods/calendar.cfm).

// calendarCell is one table cell. A padding cell is Blank; a day with a
// live entry has a URL.
type calendarCell struct {
	Blank bool
	Day   int
	URL   string
	Today bool
}

// calendarView is what templates/calendar.html draws.
type calendarView struct {
	MonthTitle string
	MonthURL   string
	PrevURL    string
	NextURL    string
	DayNames   []string
	Weeks      [][]calendarCell
}

func (m *Module) calendarPod(c *podCtx) (string, template.HTML) {
	title := c.bundle.T("calendar")
	year, month := calendarMonth(c.req, c.now)
	days, err := m.store.DaysWithEntries(c.ctx, year, month, c.loc)
	if err != nil {
		return title, fail(Calendar, err)
	}
	return title, m.exec("calendar", buildCalendar(c, year, month, days))
}

// buildCalendar lays the month out. The week starts on the locale's first
// day (i18n.FirstDayOfWeek), which is calendar.cfm's getFirstWeekPAD.
func buildCalendar(c *podCtx, year int, month time.Month, days []int) calendarView {
	locale := c.bundle.Locale()
	first := time.Date(year, month, 1, 0, 0, 0, 0, c.loc)
	firstDay := i18n.FirstDayOfWeek(locale)

	v := calendarView{
		MonthTitle: i18n.FormatDate(locale, first, i18n.StyleMonthYear),
		MonthURL:   monthURL(c.base, year, month),
		PrevURL:    monthURL(c.base, first.AddDate(0, -1, 0).Year(), first.AddDate(0, -1, 0).Month()),
		NextURL:    monthURL(c.base, first.AddDate(0, 1, 0).Year(), first.AddDate(0, 1, 0).Month()),
	}
	for i := 0; i < 7; i++ {
		v.DayNames = append(v.DayNames, i18n.ShortDay(locale, time.Weekday((int(firstDay)+i)%7)))
	}

	active := map[int]bool{}
	for _, d := range days {
		active[d] = true
	}
	// today is only marked when the grid is this month, as calendar.cfm's
	// month/year comparison does. c.now is already in the blog's zone.
	today := 0
	if c.now.Year() == year && c.now.Month() == month {
		today = c.now.Day()
	}

	pad := (int(first.Weekday()) - int(firstDay) + 7) % 7
	week := make([]calendarCell, 0, 7)
	for i := 0; i < pad; i++ {
		week = append(week, calendarCell{Blank: true})
	}
	last := first.AddDate(0, 1, -1).Day()
	for day := 1; day <= last; day++ {
		cell := calendarCell{Day: day, Today: day == today}
		if active[day] {
			cell.URL = monthURL(c.base, year, month) + "/" + strconv.Itoa(day)
		}
		week = append(week, cell)
		if len(week) == 7 {
			v.Weeks = append(v.Weeks, week)
			week = make([]calendarCell, 0, 7)
		}
	}
	if len(week) > 0 {
		for len(week) < 7 {
			week = append(week, calendarCell{Blank: true})
		}
		v.Weeks = append(v.Weeks, week)
	}
	return v
}

// monthURL is the month archive, `{base}/{year}/{month}` with no zero
// padding: the day links, the title link and the prev/next links all hang
// off it, as calendar.cfm's blogurl links do.
func monthURL(base string, year int, month time.Month) string {
	return base + "/" + strconv.Itoa(year) + "/" + strconv.Itoa(int(month))
}

// calendarMonth is the month the calendar draws: `?year=&month=` when the
// query carries them, else the month in the SES archive path the reader
// is on (/{year}/{month}[/{day}[/{alias}]]), else this month in the
// blog's zone (PLAN §9 D01, §11 "Timezone").
func calendarMonth(r *http.Request, now time.Time) (int, time.Month) {
	if r != nil {
		q := r.URL.Query()
		if y, mo, ok := parseYearMonth(q.Get("year"), q.Get("month")); ok {
			return y, mo
		}
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) >= 2 {
			if y, mo, ok := parseYearMonth(parts[0], parts[1]); ok {
				return y, mo
			}
		}
	}
	return now.Year(), now.Month()
}

// parseYearMonth accepts a four-digit year and a month of 1-12; anything
// else is not a month request.
func parseYearMonth(rawYear, rawMonth string) (int, time.Month, bool) {
	year, err := strconv.Atoi(strings.TrimSpace(rawYear))
	if err != nil || year < 1000 || year > 9999 {
		return 0, 0, false
	}
	month, err := strconv.Atoi(strings.TrimSpace(rawMonth))
	if err != nil || month < 1 || month > 12 {
		return 0, 0, false
	}
	return year, time.Month(month), true
}
