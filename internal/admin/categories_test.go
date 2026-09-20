package admin_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/4scottt/go-blogcfc/internal/store"
)

// TestFP_A12_CategoriesCRUDAliasValidationDuplicateRefusedDeletePurges
// covers PLAN §9 A12 (admin/categories.cfm, admin/category.cfm): the list,
// create and edit, the alias made from the name and validated against
// PLAN §18's reserved paths, a refused duplicate, and a delete that takes
// the entry links with it.
func TestFP_A12_CategoriesCRUDAliasValidationDuplicateRefusedDeletePurges(t *testing.T) {
	h := newHarness(t)
	h.user("admin", "Admin")
	h.user("writer", "AddCategory")
	ctx := context.Background()

	// The screen is behind ManageCategories, which the writer lacks.
	h.login("writer")
	resp, _ := h.get("/admin/categories")
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("a user without ManageCategories: status %d, want 403", resp.StatusCode)
	}
	h.get("/admin/logout")
	h.login("admin")

	// Create: a blank alias is made from the name.
	resp, _ = h.postForm("/admin/categories/new", url.Values{
		"name": {"Cold Fusion"}, "alias": {""}, "save": {"Save"},
	})
	redirectedTo(t, resp, "/admin/categories?saved=1")
	cf, err := h.store.GetCategoryByAlias(ctx, "Cold-Fusion")
	if err != nil {
		t.Fatalf("the category was not created with an alias from its name: %v", err)
	}

	// The list shows the name as a link, the alias, the count and a delete
	// button per row, with the Add Category link above them.
	entry := h.createEntry(&store.Entry{Title: "Counted", Alias: "counted", Body: "b",
		Posted: time.Now().UTC().Add(-time.Hour), Username: "admin", Released: true})
	if err := h.store.SetEntryCategories(ctx, entry.ID, []string{cf.ID}); err != nil {
		t.Fatalf("SetEntryCategories: %v", err)
	}
	resp, body := h.get("/admin/categories")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("categories: status %d, want 200", resp.StatusCode)
	}
	for _, want := range []string{
		"<h1>Categories</h1>",
		`<a href="/admin/categories/` + cf.ID + `">Cold Fusion</a>`,
		"<td>Cold-Fusion</td>",
		`<td class="number">1</td>`,
		`href="/admin/categories/new"`,
		`action="/admin/categories/` + cf.ID + `/delete"`,
		`value="Delete"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the categories list is missing %s", want)
		}
	}

	// A reserved first segment is refused (PLAN §18).
	resp, body = h.postForm("/admin/categories/new", url.Values{
		"name": {"Search Stuff"}, "alias": {"search"}, "save": {"Save"},
	})
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "a path the blog itself uses") {
		t.Errorf("the reserved alias `search`: status %d, want the form back with a message", resp.StatusCode)
	}
	if _, err := h.store.GetCategoryByAlias(ctx, "search"); !errors.Is(err, store.ErrNotFound) {
		t.Error("a category was created with the reserved alias `search`")
	}

	// So is an alias with characters a URL segment should not carry.
	_, body = h.postForm("/admin/categories/new", url.Values{
		"name": {"Odd"}, "alias": {"hey there!"}, "save": {"Save"},
	})
	if !strings.Contains(body, "letters, numbers and hyphens") {
		t.Error("an alias with a space and a bang was not refused")
	}

	// A duplicate name, and a duplicate alias, each come back with a
	// message rather than a 500.
	_, body = h.postForm("/admin/categories/new", url.Values{"name": {"cold fusion"}, "save": {"Save"}})
	if !strings.Contains(body, "name already exists") {
		t.Error("a duplicate category name was not refused")
	}
	_, body = h.postForm("/admin/categories/new", url.Values{
		"name": {"Something Else"}, "alias": {"Cold-Fusion"}, "save": {"Save"},
	})
	if !strings.Contains(body, "already exists") {
		t.Error("a duplicate category alias was not refused")
	}

	// A blank name is refused.
	_, body = h.postForm("/admin/categories/new", url.Values{"name": {"  "}, "save": {"Save"}})
	if !strings.Contains(body, "cannot be blank") {
		t.Error("a blank category name was not refused")
	}

	// Edit: the form comes back filled, and a save rewrites both fields.
	_, body = h.get("/admin/categories/" + cf.ID)
	if !strings.Contains(body, `value="Cold Fusion"`) || !strings.Contains(body, `value="Cold-Fusion"`) {
		t.Error("the category editor does not show the stored name and alias")
	}
	for _, want := range []string{`name="name"`, `name="alias"`, `value="Save"`, "<h1>Category Editor</h1>"} {
		if !strings.Contains(body, want) {
			t.Errorf("the category editor is missing %s", want)
		}
	}
	resp, _ = h.postForm("/admin/categories/"+cf.ID, url.Values{
		"name": {"ColdFusion"}, "alias": {"ColdFusion"}, "save": {"Save"},
	})
	redirectedTo(t, resp, "/admin/categories?saved=1")
	updated, err := h.store.GetCategory(ctx, cf.ID)
	if err != nil || updated.Name != "ColdFusion" || updated.Alias != "ColdFusion" {
		t.Fatalf("the edit did not stick: %+v (%v)", updated, err)
	}

	// Delete: the row goes, and the entry's link to it with it.
	resp, _ = h.postForm("/admin/categories/"+cf.ID+"/delete", url.Values{"delete": {"Delete"}})
	redirectedTo(t, resp, "/admin/categories?deleted=1")
	if _, err := h.store.GetCategory(ctx, cf.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("the category survived the delete: %v", err)
	}
	kept, err := h.store.GetEntry(ctx, entry.ID)
	if err != nil {
		t.Fatalf("the entry went with its category: %v", err)
	}
	if len(kept.Categories) != 0 {
		t.Errorf("the entry still has %d categories, want the links purged", len(kept.Categories))
	}
}
