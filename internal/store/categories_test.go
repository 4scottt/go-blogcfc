package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/4scottt/go-blogcfc/internal/store"
	"github.com/4scottt/go-blogcfc/internal/testdb"
)

// TestCategoryCRUDRefusesDuplicatesAndPurgesLinks is the category
// screen's contract: unique name and alias, live counts, and a delete that
// leaves no dangling links.
func TestCategoryCRUDRefusesDuplicatesAndPurgesLinks(t *testing.T) {
	st := testdb.New(t)
	ctx := context.Background()
	now := time.Now().UTC()

	support := &store.Category{Name: "Support", Alias: "support"}
	if err := st.CreateCategory(ctx, support); err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}
	if support.ID == "" {
		t.Fatal("CreateCategory did not fill the id")
	}

	if err := st.CreateCategory(ctx, &store.Category{Name: "Support", Alias: "support-2"}); !errors.Is(err, store.ErrDuplicate) {
		t.Fatalf("a duplicate name gave %v, want ErrDuplicate", err)
	}
	if err := st.CreateCategory(ctx, &store.Category{Name: "Other", Alias: "support"}); !errors.Is(err, store.ErrDuplicate) {
		t.Fatalf("a duplicate alias gave %v, want ErrDuplicate", err)
	}

	news := &store.Category{Name: "News", Alias: "news"}
	if err := st.CreateCategory(ctx, news); err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}

	live := mustCreateEntry(t, st, &store.Entry{Title: "Live", Alias: "live", Body: "b",
		Posted: now.Add(-time.Hour), Username: "admin", Released: true})
	draft := mustCreateEntry(t, st, &store.Entry{Title: "Draft", Alias: "draft", Body: "b",
		Posted: now.Add(-time.Hour), Username: "admin"})
	for _, id := range []string{live.ID, draft.ID} {
		if err := st.SetEntryCategories(ctx, id, []string{support.ID}); err != nil {
			t.Fatalf("SetEntryCategories: %v", err)
		}
	}

	cats, err := st.ListCategories(ctx)
	if err != nil {
		t.Fatalf("ListCategories: %v", err)
	}
	if len(cats) != 2 || cats[0].Name != "News" || cats[1].Name != "Support" {
		t.Fatalf("categories = %+v, want News then Support", cats)
	}
	if cats[1].EntryCount != 1 {
		t.Fatalf("Support counts %d entries, want 1: the draft does not count", cats[1].EntryCount)
	}

	// Listing entries by category uses the same links.
	rows, total, err := st.ListEntries(ctx, store.EntryFilter{LiveOnly: true, CategoryIDs: []string{support.ID}})
	if err != nil || total != 1 || rows[0].ID != live.ID {
		t.Fatalf("by category = %d rows (total %d, err %v)", len(rows), total, err)
	}

	byAlias, err := st.GetCategoryByAlias(ctx, "support")
	if err != nil || byAlias.ID != support.ID {
		t.Fatalf("GetCategoryByAlias: %+v %v", byAlias, err)
	}
	if _, err := st.GetCategoryByAlias(ctx, "nope"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing alias gave %v, want ErrNotFound", err)
	}

	support.Name, support.Alias = "Help", "help"
	if err := st.UpdateCategory(ctx, support); err != nil {
		t.Fatalf("UpdateCategory: %v", err)
	}
	got, err := st.GetCategory(ctx, support.ID)
	if err != nil || got.Name != "Help" || got.Alias != "help" {
		t.Fatalf("after the update: %+v %v", got, err)
	}
	if err := st.UpdateCategory(ctx, &store.Category{ID: news.ID, Name: "Help", Alias: "news"}); !errors.Is(err, store.ErrDuplicate) {
		t.Fatalf("renaming onto a taken name gave %v, want ErrDuplicate", err)
	}

	if err := st.DeleteCategory(ctx, support.ID); err != nil {
		t.Fatalf("DeleteCategory: %v", err)
	}
	if _, err := st.GetCategory(ctx, support.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("the category survived the delete: %v", err)
	}
	var links int
	if err := st.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM entry_categories WHERE category_id = ?", support.ID).Scan(&links); err != nil {
		t.Fatalf("count entry_categories: %v", err)
	}
	if links != 0 {
		t.Fatalf("entry links left behind: %d", links)
	}
	after, err := st.GetEntry(ctx, live.ID)
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	if len(after.Categories) != 0 {
		t.Fatalf("the entry still lists %+v", after.Categories)
	}
}
