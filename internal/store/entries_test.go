package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/4scottt/go-blogcfc/internal/store"
	"github.com/4scottt/go-blogcfc/internal/testdb"
)

func mustCreateEntry(t *testing.T, st *store.Store, e *store.Entry) *store.Entry {
	t.Helper()
	if err := st.CreateEntry(context.Background(), e); err != nil {
		t.Fatalf("CreateEntry(%s): %v", e.Title, err)
	}
	return e
}

// TestListEntriesLiveOnlyHidesDraftsAndFutureEntries: the three
// entry states (PLAN §11) as the public list sees them.
func TestListEntriesLiveOnlyHidesDraftsAndFutureEntries(t *testing.T) {
	st := testdb.New(t)
	ctx := context.Background()
	now := time.Now().UTC()

	live := mustCreateEntry(t, st, &store.Entry{Title: "Live", Alias: "live", Body: "body",
		Posted: now.Add(-time.Hour), Username: "admin", Released: true, AllowComments: true})
	mustCreateEntry(t, st, &store.Entry{Title: "Draft", Alias: "draft", Body: "body",
		Posted: now.Add(-2 * time.Hour), Username: "admin", Released: false})
	mustCreateEntry(t, st, &store.Entry{Title: "Scheduled", Alias: "scheduled", Body: "body",
		Posted: now.Add(24 * time.Hour), Username: "admin", Released: true})

	rows, total, err := st.ListEntries(ctx, store.EntryFilter{LiveOnly: true, Sort: "posted", Desc: true})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if total != 1 || len(rows) != 1 || rows[0].ID != live.ID {
		t.Fatalf("live list = %d rows (total %d), want only %q", len(rows), total, live.Title)
	}
	if !rows[0].Live(now) {
		t.Error("the live entry does not report itself live")
	}

	all, total, err := st.ListEntries(ctx, store.EntryFilter{})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if total != 3 || len(all) != 3 {
		t.Fatalf("admin list = %d rows (total %d), want 3", len(all), total)
	}

	draftsOnly := false
	drafts, total, err := st.ListEntries(ctx, store.EntryFilter{Released: &draftsOnly})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if total != 1 || drafts[0].Title != "Draft" {
		t.Fatalf("drafts = %+v (total %d)", drafts, total)
	}
}

// TestListEntriesKeywordsSortAndPaging covers the admin list's
// filter, its sort columns and its paging.
func TestListEntriesKeywordsSortAndPaging(t *testing.T) {
	st := testdb.New(t)
	ctx := context.Background()
	now := time.Now().UTC()

	mustCreateEntry(t, st, &store.Entry{Title: "Alpha", Alias: "alpha", Body: "about hosting",
		Posted: now.Add(-3 * time.Hour), Username: "admin", Released: true, Views: 5})
	mustCreateEntry(t, st, &store.Entry{Title: "Beta", Alias: "beta", Body: "plain", MoreBody: "more about hosting",
		Posted: now.Add(-2 * time.Hour), Username: "editor", Released: true, Views: 50})
	mustCreateEntry(t, st, &store.Entry{Title: "Gamma", Alias: "gamma", Body: "nothing to see",
		Posted: now.Add(-time.Hour), Username: "admin", Released: true, Views: 1})

	hits, total, err := st.ListEntries(ctx, store.EntryFilter{Keywords: "hosting", Sort: "title"})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if total != 2 || hits[0].Title != "Alpha" || hits[1].Title != "Beta" {
		t.Fatalf("keyword hits = %+v (total %d), want Alpha and Beta", titles(hits), total)
	}

	// The keyword is a literal: a LIKE wildcard in it matches nothing.
	if _, total, err := st.ListEntries(ctx, store.EntryFilter{Keywords: "%"}); err != nil || total != 0 {
		t.Fatalf("a bare %% matched %d rows (err %v), want 0", total, err)
	}

	byViews, _, err := st.ListEntries(ctx, store.EntryFilter{Sort: "views", Desc: true})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if byViews[0].Title != "Beta" || byViews[2].Title != "Gamma" {
		t.Fatalf("by views = %v", titles(byViews))
	}

	byUser, total, err := st.ListEntries(ctx, store.EntryFilter{Username: "admin", Sort: "title"})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if total != 2 || byUser[0].Username != "admin" {
		t.Fatalf("posted-by list = %v (total %d)", titles(byUser), total)
	}

	page, total, err := st.ListEntries(ctx, store.EntryFilter{Sort: "posted", Desc: true, Limit: 2, Offset: 1})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if total != 3 || len(page) != 2 || page[0].Title != "Beta" {
		t.Fatalf("page = %v (total %d), want the second and third newest", titles(page), total)
	}

	// An unknown sort falls back to posted rather than reaching the SQL.
	if _, _, err := st.ListEntries(ctx, store.EntryFilter{Sort: "views; DROP TABLE entries"}); err != nil {
		t.Fatalf("an unknown sort should fall back, not fail: %v", err)
	}

	from := now.Add(-150 * time.Minute)
	ranged, total, err := st.ListEntries(ctx, store.EntryFilter{From: &from})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if total != 2 {
		t.Fatalf("date range = %v (total %d), want 2", titles(ranged), total)
	}
}

// TestEntryCreateReadUpdateDelete is the admin editor's round trip,
// including the category links and the purge on delete.
func TestEntryCreateReadUpdateDelete(t *testing.T) {
	st := testdb.New(t)
	ctx := context.Background()

	cat := &store.Category{Name: "Support", Alias: "support"}
	if err := st.CreateCategory(ctx, cat); err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}

	e := mustCreateEntry(t, st, &store.Entry{
		Title: "Welcome to the rewrite", Alias: "welcome-to-the-rewrite",
		Body: "The body", MoreBody: "The rest", Username: "admin",
		Released: true, AllowComments: true, SendEmail: true,
		Enclosure: "talk.mp3", FileSize: 4096, MimeType: "audio/mpeg",
		Subtitle: "A subtitle", Summary: "A summary", Keywords: "go, rewrite", Duration: "00:12:00",
	})
	if e.ID == "" {
		t.Fatal("CreateEntry did not fill the id")
	}
	if e.Posted.IsZero() || e.Posted.Location() != time.UTC {
		t.Fatalf("posted = %v, want a UTC default", e.Posted)
	}

	if err := st.SetEntryCategories(ctx, e.ID, []string{cat.ID, cat.ID}); err != nil {
		t.Fatalf("SetEntryCategories: %v", err)
	}

	got, err := st.GetEntry(ctx, e.ID)
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	if got.Title != e.Title || got.MoreBody != "The rest" || !got.SendEmail || got.FileSize != 4096 {
		t.Fatalf("GetEntry returned %+v", got)
	}
	if len(got.Categories) != 1 || got.Categories[0].Name != "Support" {
		t.Fatalf("categories = %+v, want one Support (duplicates collapsed)", got.Categories)
	}
	if !got.Posted.Equal(e.Posted) {
		t.Errorf("posted round trip: %v != %v", got.Posted, e.Posted)
	}

	byAlias, err := st.GetEntryByAlias(ctx, "welcome-to-the-rewrite")
	if err != nil || byAlias.ID != e.ID {
		t.Fatalf("GetEntryByAlias: %+v %v", byAlias, err)
	}
	if _, err := st.GetEntryByAlias(ctx, "no-such-alias"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing alias gave %v, want ErrNotFound", err)
	}
	if _, err := st.GetEntry(ctx, "00000000-0000-4000-8000-000000000000"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing id gave %v, want ErrNotFound", err)
	}

	got.Title = "Edited"
	got.Released = false
	if err := st.UpdateEntry(ctx, got); err != nil {
		t.Fatalf("UpdateEntry: %v", err)
	}
	if err := st.IncrementViews(ctx, e.ID); err != nil {
		t.Fatalf("IncrementViews: %v", err)
	}
	reread, err := st.GetEntry(ctx, e.ID)
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	if reread.Title != "Edited" || reread.Released || reread.Views != 1 {
		t.Fatalf("after the update: %+v", reread)
	}

	if err := st.UpdateEntry(ctx, &store.Entry{ID: "00000000-0000-4000-8000-000000000000"}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("updating a missing entry gave %v, want ErrNotFound", err)
	}

	if err := st.DeleteEntries(ctx, []string{e.ID}); err != nil {
		t.Fatalf("DeleteEntries: %v", err)
	}
	if _, err := st.GetEntry(ctx, e.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("the entry survived the delete: %v", err)
	}
	var links int
	if err := st.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM entry_categories WHERE entry_id = ?", e.ID).Scan(&links); err != nil {
		t.Fatalf("count entry_categories: %v", err)
	}
	if links != 0 {
		t.Fatalf("category links left behind: %d", links)
	}
}

func titles(entries []store.Entry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Title)
	}
	return out
}
