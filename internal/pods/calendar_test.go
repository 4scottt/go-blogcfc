package pods

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/4scottt/go-blogcfc/internal/store"
)

// TestFP_D01_CalendarMonthGrid: the calendar pod draws the month the
// reader is on, in the blog's zone, with the days that have a live entry
// linked, today highlighted and the month's neighbours one click away
// (PLAN §9 D01, client/includes/pods/calendar.cfm).
func TestFP_D01_CalendarMonthGrid(t *testing.T) {
	s := newTestSite(t)
	s.only(Calendar)

	// 2025-09-02 02:00Z is 2025-09-01 19:00 in PDT: the entry belongs to
	// the first, not the second (PLAN §11 "Timezone").
	s.entry(store.Entry{Title: "First", Alias: "first", Released: true,
		Posted: time.Date(2025, time.September, 2, 2, 0, 0, 0, time.UTC)})
	s.entry(store.Entry{Title: "Fifteenth", Alias: "fifteenth", Released: true,
		Posted: time.Date(2025, time.September, 15, 20, 0, 0, 0, time.UTC)})
	// A draft and a scheduled entry are not days on the calendar.
	s.entry(store.Entry{Title: "Draft", Alias: "draft", Released: false,
		Posted: time.Date(2025, time.September, 9, 20, 0, 0, 0, time.UTC)})
	s.entry(store.Entry{Title: "Scheduled", Alias: "scheduled", Released: true,
		Posted: time.Now().AddDate(1, 0, 0)})

	got := s.sidebar("/?year=2025&month=9")
	checkGolden(t, "calendar.html", got)

	mustContain(t, got,
		`<h4>Calendar</h4>`,
		`<a href="`+testBase+`/2025/9/1" rel="nofollow">1</a>`,
		`<a href="`+testBase+`/2025/9/15" rel="nofollow">15</a>`,
		// The month's neighbours, as calendar.cfm links them: the month
		// archive itself, not a query string.
		`<a href="`+testBase+`/2025/8" rel="nofollow">&lt;&lt;</a>`,
		`<a href="`+testBase+`/2025/10" rel="nofollow">&gt;&gt;</a>`,
		`<a href="`+testBase+`/2025/9" rel="nofollow">September 2025</a>`,
	)
	// Neither the draft's day nor the scheduled entry's is a link.
	mustNotContain(t, got, testBase+"/2025/9/9")

	// The same month reached through the SES archive path.
	if ses := s.sidebar("/2025/9/15/fifteenth"); ses != got {
		t.Errorf("the SES path draws a different calendar than ?year=&month=\n--- ses ---\n%s\n--- query ---\n%s", ses, got)
	}

	// A month the reader did not ask for is this month, and today is
	// marked in the blog's zone.
	now := time.Now().In(s.loc)
	home := s.sidebar("/")
	mustContain(t, home,
		`<a href="`+monthURL(testBase, now.Year(), now.Month())+`" rel="nofollow">`,
		`<td class="calendarToday">`,
	)
	if !strings.Contains(home, `<td class="calendarToday">`+itoa(now.Day())+`</td>`) &&
		!strings.Contains(home, `<td class="calendarToday"><a href="`+monthURL(testBase, now.Year(), now.Month())+"/"+itoa(now.Day())) {
		t.Errorf("today (%d) is not the highlighted cell:\n%s", now.Day(), home)
	}

	// The week starts where the locale starts it (PLAN §9 R06): Sunday
	// in en_US, Monday in de_DE.
	if err := s.settings.Set(context.Background(), map[string]string{"locale": "de_DE"}); err != nil {
		t.Fatalf("set locale: %v", err)
	}
	german := s.sidebar("/?year=2025&month=9")
	mustContain(t, german, "<tr><th>Mo</th><th>Di</th>", "September 2025")
	mustNotContain(t, german, "<tr><th>Sun</th>")
}

// TestFP_D01_CalendarMonthRequest: which month the grid draws, from the
// query, from the SES archive path, or neither.
func TestFP_D01_CalendarMonthRequest(t *testing.T) {
	now := time.Date(2025, time.September, 19, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		path  string
		year  int
		month time.Month
	}{
		{"query", "/?year=2024&month=3", 2024, time.March},
		{"month archive path", "/2019/11", 2019, time.November},
		{"day archive path", "/2019/11/4", 2019, time.November},
		{"entry permalink", "/2019/11/4/some-entry", 2019, time.November},
		{"query beats path", "/2019/11?year=2024&month=3", 2024, time.March},
		{"home", "/", now.Year(), now.Month()},
		{"a category alias is not a month", "/coldfusion", now.Year(), now.Month()},
		{"a month out of range falls back", "/?year=2024&month=13", now.Year(), now.Month()},
		{"a year out of range falls back", "/?year=24&month=3", now.Year(), now.Month()},
		{"a postedby path is not a month", "/postedby/admin", now.Year(), now.Month()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRequest(tc.path)
			year, month := calendarMonth(r, now)
			if year != tc.year || month != tc.month {
				t.Errorf("calendarMonth(%q) = %d-%s, want %d-%s", tc.path, year, month, tc.year, tc.month)
			}
		})
	}
}
