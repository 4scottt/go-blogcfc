package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/4scottt/go-blogcfc/internal/store"
	"github.com/4scottt/go-blogcfc/internal/testdb"
)

// mustLoadLocation is the blog's zone for the archive tests. The grouping
// has to happen in the zone, not at a fixed offset.
func mustLoadLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("zone %s is not available: %v", name, err)
	}
	return loc
}

// TestMonthlyArchivesGroupInTheBlogsZoneAcrossDST: two entries posted
// minutes into a UTC month belong to the previous month for a reader in
// Los Angeles, and the offset that decides it is -08:00 in January and
// -07:00 in July. A single CONVERT_TZ offset in SQL would put one of them
// in the wrong month.
func TestMonthlyArchivesGroupInTheBlogsZoneAcrossDST(t *testing.T) {
	st := testdb.New(t)
	ctx := context.Background()
	la := mustLoadLocation(t, "America/Los_Angeles")

	// 2026-01-01 07:30Z is 2025-12-31 23:30 in PST (-08:00).
	mustCreateEntry(t, st, &store.Entry{Title: "New year", Alias: "new-year", Body: "b",
		Posted: time.Date(2026, time.January, 1, 7, 30, 0, 0, time.UTC), Username: "admin", Released: true})
	// 2026-07-01 03:00Z is 2026-06-30 20:00 in PDT (-07:00).
	mustCreateEntry(t, st, &store.Entry{Title: "Midsummer", Alias: "midsummer", Body: "b",
		Posted: time.Date(2026, time.July, 1, 3, 0, 0, 0, time.UTC), Username: "admin", Released: true})
	mustCreateEntry(t, st, &store.Entry{Title: "Also June", Alias: "also-june", Body: "b",
		Posted: time.Date(2026, time.June, 15, 18, 0, 0, 0, time.UTC), Username: "admin", Released: true})
	mustCreateEntry(t, st, &store.Entry{Title: "Draft", Alias: "draft-archive", Body: "b",
		Posted: time.Date(2026, time.June, 16, 18, 0, 0, 0, time.UTC), Username: "admin"})
	mustCreateEntry(t, st, &store.Entry{Title: "Long ago", Alias: "long-ago", Body: "b",
		Posted: time.Date(2019, time.May, 5, 12, 0, 0, 0, time.UTC), Username: "admin", Released: true})

	months, err := st.MonthlyArchives(ctx, 0, la)
	if err != nil {
		t.Fatalf("MonthlyArchives: %v", err)
	}
	want := []store.MonthCount{
		{Year: 2026, Month: time.June, Count: 2},
		{Year: 2025, Month: time.December, Count: 1},
		{Year: 2019, Month: time.May, Count: 1},
	}
	if len(months) != len(want) {
		t.Fatalf("months = %+v, want %+v", months, want)
	}
	for i := range want {
		if months[i] != want[i] {
			t.Fatalf("months = %+v, want %+v", months, want)
		}
	}

	// In UTC the same rows fall in different months.
	utcMonths, err := st.MonthlyArchives(ctx, 0, time.UTC)
	if err != nil {
		t.Fatalf("MonthlyArchives(UTC): %v", err)
	}
	if utcMonths[0].Month != time.July || utcMonths[0].Count != 1 {
		t.Fatalf("in UTC the newest month is %+v, want one entry in July", utcMonths[0])
	}

	// `years` is BlogCFC's archiveYears: entries from (this year - years).
	recent, err := st.MonthlyArchives(ctx, 5, la)
	if err != nil {
		t.Fatalf("MonthlyArchives(5): %v", err)
	}
	for _, m := range recent {
		if m.Year == 2019 {
			t.Fatalf("a five-year window still holds 2019: %+v", recent)
		}
	}
	if len(recent) != 2 {
		t.Fatalf("five-year window = %+v, want the two recent months", recent)
	}
	if nilLoc, err := st.MonthlyArchives(ctx, 0, nil); err != nil || len(nilLoc) != len(utcMonths) {
		t.Fatalf("a nil zone = %+v (%v), want the UTC grouping", nilLoc, err)
	}
}

// TestDaysWithEntriesUsesTheBlogsZone: the calendar's active days, with
// the month's edges read in the zone rather than in UTC.
func TestDaysWithEntriesUsesTheBlogsZone(t *testing.T) {
	st := testdb.New(t)
	ctx := context.Background()
	la := mustLoadLocation(t, "America/Los_Angeles")

	// 2026-07-01 03:00Z is still June 30 in Los Angeles.
	mustCreateEntry(t, st, &store.Entry{Title: "June still", Alias: "june-still", Body: "b",
		Posted: time.Date(2026, time.July, 1, 3, 0, 0, 0, time.UTC), Username: "admin", Released: true})
	// 2026-08-01 02:00Z is July 31 in Los Angeles.
	mustCreateEntry(t, st, &store.Entry{Title: "July still", Alias: "july-still", Body: "b",
		Posted: time.Date(2026, time.August, 1, 2, 0, 0, 0, time.UTC), Username: "admin", Released: true})
	mustCreateEntry(t, st, &store.Entry{Title: "Mid July", Alias: "mid-july", Body: "b",
		Posted: time.Date(2026, time.July, 15, 20, 0, 0, 0, time.UTC), Username: "admin", Released: true})
	mustCreateEntry(t, st, &store.Entry{Title: "Also mid July", Alias: "also-mid-july", Body: "b",
		Posted: time.Date(2026, time.July, 15, 21, 0, 0, 0, time.UTC), Username: "admin", Released: true})
	mustCreateEntry(t, st, &store.Entry{Title: "July draft", Alias: "july-draft", Body: "b",
		Posted: time.Date(2026, time.July, 20, 20, 0, 0, 0, time.UTC), Username: "admin"})

	days, err := st.DaysWithEntries(ctx, 2026, time.July, la)
	if err != nil {
		t.Fatalf("DaysWithEntries: %v", err)
	}
	if len(days) != 2 || days[0] != 15 || days[1] != 31 {
		t.Fatalf("July days = %v, want [15 31]: the 1st belongs to June and the draft does not count", days)
	}

	june, err := st.DaysWithEntries(ctx, 2026, time.June, la)
	if err != nil || len(june) != 1 || june[0] != 30 {
		t.Fatalf("June days = %v (%v), want [30]", june, err)
	}
	if empty, err := st.DaysWithEntries(ctx, 2026, time.February, la); err != nil || len(empty) != 0 {
		t.Fatalf("a month with nothing in it = %v (%v)", empty, err)
	}
	if utcDays, err := st.DaysWithEntries(ctx, 2026, time.July, nil); err != nil || len(utcDays) != 2 || utcDays[0] != 1 {
		t.Fatalf("in UTC July's days = %v (%v), want the 1st and the 15th", utcDays, err)
	}
}

// TestEntriesForSitemapIsLiveNewestFirst.
func TestEntriesForSitemapIsLiveNewestFirst(t *testing.T) {
	st := testdb.New(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	older := mustCreateEntry(t, st, &store.Entry{Title: "Older", Alias: "older", Body: "b",
		Posted: now.Add(-48 * time.Hour), Username: "admin", Released: true})
	newer := mustCreateEntry(t, st, &store.Entry{Title: "Newer", Alias: "newer", Body: "b",
		Posted: now.Add(-time.Hour), Username: "admin", Released: true})
	mustCreateEntry(t, st, &store.Entry{Title: "Draft", Alias: "draft-sitemap", Body: "b",
		Posted: now.Add(-2 * time.Hour), Username: "admin"})
	mustCreateEntry(t, st, &store.Entry{Title: "Scheduled", Alias: "scheduled-sitemap", Body: "b",
		Posted: now.Add(48 * time.Hour), Username: "admin", Released: true})

	entries, err := st.EntriesForSitemap(ctx)
	if err != nil {
		t.Fatalf("EntriesForSitemap: %v", err)
	}
	if len(entries) != 2 || entries[0].ID != newer.ID || entries[1].ID != older.ID {
		t.Fatalf("sitemap entries = %+v, want the two live ones newest first", entries)
	}
}
