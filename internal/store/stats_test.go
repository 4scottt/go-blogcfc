package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/4scottt/go-blogcfc/internal/store"
	"github.com/4scottt/go-blogcfc/internal/testdb"
)

// TestFP_A02_TopEntriesByViewsWindowAndOrder: the dashboard's query
// (PLAN §9 A02) takes the window from `posted` and orders by views.
func TestFP_A02_TopEntriesByViewsWindowAndOrder(t *testing.T) {
	st := testdb.New(t)
	ctx := context.Background()
	now := time.Now().UTC()

	for _, e := range []*store.Entry{
		{Title: "Third", Alias: "third", Body: "b", Posted: now.Add(-time.Hour), Username: "admin", Released: true, Views: 1},
		{Title: "First", Alias: "first", Body: "b", Posted: now.Add(-2 * time.Hour), Username: "admin", Released: true, Views: 30},
		{Title: "Second", Alias: "second", Body: "b", Posted: now.Add(-3 * time.Hour), Username: "admin", Released: true, Views: 20},
		{Title: "Older", Alias: "older", Body: "b", Posted: now.Add(-8 * 24 * time.Hour), Username: "admin", Released: true, Views: 900},
	} {
		if err := st.CreateEntry(ctx, e); err != nil {
			t.Fatalf("CreateEntry(%s): %v", e.Title, err)
		}
	}

	got, err := st.TopEntriesByViews(ctx, now.Add(-7*24*time.Hour), 5)
	if err != nil {
		t.Fatalf("TopEntriesByViews: %v", err)
	}
	want := []string{"First", "Second", "Third"}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d (the 8-day-old one is outside the window)", len(got), len(want))
	}
	for i, title := range want {
		if got[i].Title != title {
			t.Errorf("entry %d = %q, want %q", i, got[i].Title, title)
		}
	}

	limited, err := st.TopEntriesByViews(ctx, now.Add(-7*24*time.Hour), 2)
	if err != nil {
		t.Fatalf("TopEntriesByViews(limit 2): %v", err)
	}
	if len(limited) != 2 || limited[0].Title != "First" {
		t.Fatalf("limit 2 returned %d entries starting %v", len(limited), limited)
	}
}
