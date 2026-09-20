package admin_test

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/4scottt/go-blogcfc/internal/store"
)

// TestFP_A28_StatsGeneralTopViewsCategoriesCommentsSearchCommentersByYear
// covers PLAN §9 A28 (admin/stats.cfm, admin/statsbyyear.cfm): the
// seven sections of the stats screen, and the same seven bounded by a
// year at /admin/stats/{year}.
func TestFP_A28_StatsGeneralTopViewsCategoriesCommentsSearchCommentersByYear(t *testing.T) {
	h := newHarness(t)
	h.user("admin", "Admin")
	ctx := context.Background()

	thisYear := time.Now().UTC().Year()
	old := time.Date(thisYear-1, 6, 1, 12, 0, 0, 0, time.UTC)
	recent := time.Date(thisYear, 1, 2, 12, 0, 0, 0, time.UTC)

	cat := h.createCategory("ColdFusion", "coldfusion")
	popular := h.createEntry(&store.Entry{Title: "This year's popular entry", Alias: "popular", Body: "b",
		Posted: recent, Username: "admin", Released: true, Views: 90})
	ancient := h.createEntry(&store.Entry{Title: "Last year's entry", Alias: "ancient", Body: "b",
		Posted: old, Username: "admin", Released: true, Views: 500})
	if err := h.store.SetEntryCategories(ctx, popular.ID, []string{cat.ID}); err != nil {
		t.Fatalf("SetEntryCategories: %v", err)
	}
	for _, c := range []*store.Comment{
		{EntryID: popular.ID, Name: "Ann Commenter", Email: "ann@example.com", Comment: "hi", Posted: recent, Moderated: true},
		{EntryID: ancient.ID, Name: "Bob Oldtimer", Email: "bob@example.com", Comment: "hi", Posted: old, Moderated: true},
	} {
		if err := h.store.CreateComment(ctx, c); err != nil {
			t.Fatalf("CreateComment: %v", err)
		}
	}
	if err := h.store.LogSearch(ctx, "coldfusion"); err != nil {
		t.Fatalf("LogSearch: %v", err)
	}

	h.login("admin")
	resp, body := h.get("/admin/stats")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stats: status %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "<h1>Stats</h1>") {
		t.Error("the stats screen has no Stats heading")
	}
	for _, section := range []string{
		"General", "Top Entries by Views", "Category Stats",
		"Top Entries by Comments", "Top Categories by Comments",
		"Top Search Terms", "Top Commenters",
	} {
		if !strings.Contains(body, ">"+section+"</h2>") {
			t.Errorf("the stats screen has no %q section", section)
		}
	}
	for _, want := range []string{
		"This year&#39;s popular entry", "Last year&#39;s entry", "ColdFusion",
		"Ann Commenter", "Bob Oldtimer", "coldfusion",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the whole-blog stats do not mention %q", want)
		}
	}
	// General: two entries, 590 views, two comments.
	for _, want := range []string{">Entries<", ">590<", ">Comments<", ">Verified subscribers<"} {
		if !strings.Contains(body, want) {
			t.Errorf("the general table has no %q", want)
		}
	}
	if !strings.Contains(body, "/admin/stats/"+strconv.Itoa(thisYear)) {
		t.Errorf("the screen does not link to %d", thisYear)
	}

	// By year: last year's rows are gone.
	resp, body = h.get("/admin/stats/" + strconv.Itoa(thisYear))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stats by year: status %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "This year&#39;s popular entry") {
		t.Error("this year's stats do not list this year's entry")
	}
	if strings.Contains(body, "Last year&#39;s entry") {
		t.Error("this year's stats list last year's entry")
	}
	if strings.Contains(body, "Bob Oldtimer") {
		t.Error("this year's commenters include last year's")
	}
	if !strings.Contains(body, "Showing "+strconv.Itoa(thisYear)) {
		t.Error("the by-year screen does not say which year it shows")
	}

	// A year with nothing in it is a page of empty sections, not a 500.
	resp, body = h.get("/admin/stats/1999")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("an empty year: status %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "Nothing to show") {
		t.Error("an empty year does not say its tables are empty")
	}

	// Something that is not a year is not a page at all.
	if resp, _ := h.get("/admin/stats/notayear"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("/admin/stats/notayear: status %d, want 404", resp.StatusCode)
	}
}
