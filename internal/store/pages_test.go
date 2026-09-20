package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/4scottt/go-blogcfc/internal/store"
	"github.com/4scottt/go-blogcfc/internal/testdb"
)

// TestPageCRUDWithAliasAndCategories is the pages screen's contract: a
// unique alias, the layout flag, and a category set that is replaced
// wholesale and purged with the page.
func TestPageCRUDWithAliasAndCategories(t *testing.T) {
	st := testdb.New(t)
	ctx := context.Background()

	news := &store.Category{Name: "News", Alias: "news"}
	notes := &store.Category{Name: "Notes", Alias: "notes"}
	for _, c := range []*store.Category{news, notes} {
		if err := st.CreateCategory(ctx, c); err != nil {
			t.Fatalf("CreateCategory: %v", err)
		}
	}

	about := &store.Page{Title: "About", Alias: "about", Body: "<p>About me.</p>", ShowLayout: true}
	if err := st.CreatePage(ctx, about); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	if about.ID == "" {
		t.Fatal("CreatePage did not fill the id")
	}
	bare := &store.Page{Title: "Bare", Alias: "bare", Body: "no layout"}
	if err := st.CreatePage(ctx, bare); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	if err := st.CreatePage(ctx, &store.Page{Title: "Clash", Alias: "about"}); !errors.Is(err, store.ErrDuplicate) {
		t.Fatalf("a duplicate alias gave %v, want ErrDuplicate", err)
	}

	if err := st.SetPageCategories(ctx, about.ID, []string{news.ID, notes.ID, news.ID, ""}); err != nil {
		t.Fatalf("SetPageCategories: %v", err)
	}

	pages, err := st.ListPages(ctx)
	if err != nil {
		t.Fatalf("ListPages: %v", err)
	}
	if len(pages) != 2 || pages[0].Title != "About" || pages[1].Title != "Bare" {
		t.Fatalf("pages = %+v, want About then Bare", pages)
	}
	if len(pages[0].CategoryIDs) != 2 {
		t.Fatalf("About has categories %v, want two", pages[0].CategoryIDs)
	}

	byAlias, err := st.GetPageByAlias(ctx, "about")
	if err != nil || byAlias.ID != about.ID || !byAlias.ShowLayout {
		t.Fatalf("GetPageByAlias = %+v (%v)", byAlias, err)
	}
	if _, err := st.GetPageByAlias(ctx, "nope"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("an unknown alias gave %v, want ErrNotFound", err)
	}
	if got, err := st.GetPage(ctx, bare.ID); err != nil || got.ShowLayout {
		t.Fatalf("the bare page = %+v (%v), want ShowLayout false", got, err)
	}

	about.Title, about.Alias, about.ShowLayout = "About us", "about-us", false
	if err := st.UpdatePage(ctx, about); err != nil {
		t.Fatalf("UpdatePage: %v", err)
	}
	got, err := st.GetPage(ctx, about.ID)
	if err != nil || got.Title != "About us" || got.Alias != "about-us" || got.ShowLayout {
		t.Fatalf("updated page = %+v (%v)", got, err)
	}
	about.Alias = "bare"
	if err := st.UpdatePage(ctx, about); !errors.Is(err, store.ErrDuplicate) {
		t.Fatalf("taking another page's alias gave %v, want ErrDuplicate", err)
	}
	if err := st.UpdatePage(ctx, &store.Page{ID: "no-such-page", Alias: "x"}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("updating a missing page gave %v, want ErrNotFound", err)
	}

	// Replacing the set keeps only what is passed.
	if err := st.SetPageCategories(ctx, about.ID, []string{notes.ID}); err != nil {
		t.Fatalf("SetPageCategories: %v", err)
	}
	got, err = st.GetPage(ctx, about.ID)
	if err != nil || len(got.CategoryIDs) != 1 || got.CategoryIDs[0] != notes.ID {
		t.Fatalf("categories after the replace = %v (%v)", got.CategoryIDs, err)
	}

	if err := st.DeletePage(ctx, about.ID); err != nil {
		t.Fatalf("DeletePage: %v", err)
	}
	if _, err := st.GetPage(ctx, about.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("the deleted page gave %v, want ErrNotFound", err)
	}
	// The links went with it: deleting the category finds nothing to purge
	// and the remaining page is untouched.
	if err := st.DeleteCategory(ctx, notes.ID); err != nil {
		t.Fatalf("DeleteCategory: %v", err)
	}
	pages, err = st.ListPages(ctx)
	if err != nil || len(pages) != 1 || pages[0].ID != bare.ID {
		t.Fatalf("pages after the delete = %+v (%v)", pages, err)
	}
}
