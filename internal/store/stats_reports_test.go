package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/4scottt/go-blogcfc/internal/store"
	"github.com/4scottt/go-blogcfc/internal/testdb"
)

// TestStatsReportsSummaryTopsAndYearBounds covers the seven queries the
// stats screen runs (PLAN §9 A28, admin/stats.cfm): the general totals,
// the tops by views and comments, the category counts, the search terms,
// the commenters, and the bounds that make the by-year screen.
func TestStatsReportsSummaryTopsAndYearBounds(t *testing.T) {
	st := testdb.New(t)
	ctx := context.Background()

	lastYear := time.Date(time.Now().UTC().Year()-1, 6, 1, 12, 0, 0, 0, time.UTC)
	thisYear := time.Date(time.Now().UTC().Year(), 1, 2, 12, 0, 0, 0, time.UTC)

	popular := &store.Entry{Title: "Popular", Alias: "popular", Body: "b", Posted: thisYear, Username: "admin", Released: true, Views: 90}
	quiet := &store.Entry{Title: "Quiet", Alias: "quiet", Body: "b", Posted: thisYear.Add(time.Hour), Username: "admin", Released: true, Views: 4}
	old := &store.Entry{Title: "Old", Alias: "old", Body: "b", Posted: lastYear, Username: "admin", Released: true, Views: 1000}
	for _, e := range []*store.Entry{popular, quiet, old} {
		if err := st.CreateEntry(ctx, e); err != nil {
			t.Fatalf("CreateEntry(%s): %v", e.Title, err)
		}
	}
	cf := &store.Category{Name: "ColdFusion", Alias: "coldfusion"}
	golang := &store.Category{Name: "Go", Alias: "go"}
	for _, c := range []*store.Category{cf, golang} {
		if err := st.CreateCategory(ctx, c); err != nil {
			t.Fatalf("CreateCategory(%s): %v", c.Name, err)
		}
	}
	if err := st.SetEntryCategories(ctx, popular.ID, []string{cf.ID, golang.ID}); err != nil {
		t.Fatalf("SetEntryCategories: %v", err)
	}
	if err := st.SetEntryCategories(ctx, quiet.ID, []string{cf.ID}); err != nil {
		t.Fatalf("SetEntryCategories: %v", err)
	}

	// Two moderated comments on the popular entry, one held (it counts
	// nowhere), one on the old entry.
	comments := []*store.Comment{
		{EntryID: popular.ID, Name: "Ann", Email: "ann@example.com", Comment: "one", Posted: thisYear, Moderated: true},
		{EntryID: popular.ID, Name: "Ann", Email: "ann@example.com", Comment: "two", Posted: thisYear, Moderated: true},
		{EntryID: popular.ID, Name: "Spam", Email: "spam@example.com", Comment: "held", Posted: thisYear},
		{EntryID: old.ID, Name: "Bob", Email: "bob@example.com", Comment: "old", Posted: lastYear, Moderated: true},
	}
	for _, c := range comments {
		if err := st.CreateComment(ctx, c); err != nil {
			t.Fatalf("CreateComment: %v", err)
		}
	}

	from := time.Date(thisYear.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(1, 0, 0).Add(-time.Second)

	all, err := st.StatsSummary(ctx, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("StatsSummary: %v", err)
	}
	if all.Entries != 3 || all.Views != 1094 || all.Comments != 3 {
		t.Errorf("whole-blog summary = %+v, want 3 entries, 1094 views, 3 comments (the held one is not counted)", all)
	}
	if !all.FirstPosted.Equal(lastYear) || !all.LastPosted.Equal(quiet.Posted.UTC()) {
		t.Errorf("summary dates = %v..%v, want %v..%v", all.FirstPosted, all.LastPosted, lastYear, quiet.Posted)
	}

	year, err := st.StatsSummary(ctx, from, to)
	if err != nil {
		t.Fatalf("StatsSummary(year): %v", err)
	}
	if year.Entries != 2 || year.Views != 94 || year.Comments != 2 {
		t.Errorf("this year's summary = %+v, want 2 entries, 94 views, 2 comments", year)
	}

	views, err := st.TopEntriesByViewsBetween(ctx, from, to, 10)
	if err != nil {
		t.Fatalf("TopEntriesByViewsBetween: %v", err)
	}
	if len(views) != 2 || views[0].Title != "Popular" || views[0].Count != 90 {
		t.Errorf("top views = %+v, want Popular first and last year's entry out of range", views)
	}

	cats, err := st.CategoryEntryCounts(ctx, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("CategoryEntryCounts: %v", err)
	}
	if len(cats) != 2 || cats[0].Name != "ColdFusion" || cats[0].Count != 2 || cats[1].Count != 1 {
		t.Errorf("category counts = %+v, want ColdFusion 2 then Go 1", cats)
	}

	byComments, err := st.TopEntriesByComments(ctx, time.Time{}, time.Time{}, 10)
	if err != nil {
		t.Fatalf("TopEntriesByComments: %v", err)
	}
	if len(byComments) != 2 || byComments[0].Title != "Popular" || byComments[0].Count != 2 {
		t.Errorf("top by comments = %+v, want Popular with two (the held comment is not counted)", byComments)
	}

	catComments, err := st.TopCategoriesByComments(ctx, time.Time{}, time.Time{}, 10)
	if err != nil {
		t.Fatalf("TopCategoriesByComments: %v", err)
	}
	if len(catComments) != 2 || catComments[0].Count != 2 {
		t.Errorf("top categories by comments = %+v, want two apiece on the popular entry's categories", catComments)
	}

	for _, term := range []string{"go", "go", "coldfusion"} {
		if err := st.LogSearch(ctx, term); err != nil {
			t.Fatalf("LogSearch: %v", err)
		}
	}
	terms, err := st.TopSearchTermsBetween(ctx, time.Now().UTC().Add(-time.Hour), time.Time{}, 10)
	if err != nil {
		t.Fatalf("TopSearchTermsBetween: %v", err)
	}
	if len(terms) != 2 || terms[0].Term != "go" || terms[0].Count != 2 {
		t.Errorf("top terms = %+v, want go twice", terms)
	}
	none, err := st.TopSearchTermsBetween(ctx, time.Time{}, time.Now().UTC().Add(-time.Hour), 10)
	if err != nil {
		t.Fatalf("TopSearchTermsBetween(old): %v", err)
	}
	if len(none) != 0 {
		t.Errorf("a range that ends before the searches returned %+v", none)
	}

	people, err := st.TopCommenters(ctx, time.Time{}, time.Time{}, 10)
	if err != nil {
		t.Fatalf("TopCommenters: %v", err)
	}
	if len(people) != 2 || people[0].Name != "Ann" || people[0].Count != 2 {
		t.Errorf("top commenters = %+v, want Ann twice then Bob once", people)
	}

	years, err := st.EntryYears(ctx, time.UTC)
	if err != nil {
		t.Fatalf("EntryYears: %v", err)
	}
	if len(years) != 2 || years[0] != thisYear.Year() || years[1] != lastYear.Year() {
		t.Errorf("years = %v, want %d then %d", years, thisYear.Year(), lastYear.Year())
	}
}
