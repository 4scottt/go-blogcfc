package admin_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/4scottt/go-blogcfc/internal/store"
)

// TestFP_A21_PagesCRUDAliasShowlayoutCategoriesAndRole covers PLAN §9
// A21 (admin/pages.cfm, admin/page.cfm): the screen behind PageAdmin,
// the list, an alias made from the title and refused when it is taken
// or reserved, the layout flag, the category set, and a delete that
// takes the category links with it.
func TestFP_A21_PagesCRUDAliasShowlayoutCategoriesAndRole(t *testing.T) {
	h := newHarness(t)
	h.user("admin", "Admin")
	h.user("writer", "AddCategory")
	ctx := context.Background()

	// PageAdmin gates the screen; a signed-in user without it is refused.
	h.login("writer")
	resp, _ := h.get("/admin/pages")
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("a user without PageAdmin: status %d, want 403", resp.StatusCode)
	}
	h.get("/admin/logout")
	h.login("admin")
	flushes := countFlushes(h)

	cf := h.createCategory("ColdFusion", "coldfusion")
	other := h.createCategory("Go", "go")

	// The editor offers every control by name.
	_, form := h.get("/admin/pages/new")
	for _, want := range []string{
		"<h1>Page Editor</h1>", `name="title"`, `name="alias"`, `name="body"`,
		`name="showlayout"`, `name="categories"`, `value="Save"`,
	} {
		if !strings.Contains(form, want) {
			t.Errorf("the page editor has no %s", want)
		}
	}

	// A blank alias is made from the title (render.MakeTitle).
	resp, _ = h.postForm("/admin/pages/new", url.Values{
		"title": {"About This Blog"}, "alias": {""}, "body": {"Hello there."},
		"showlayout": {"1"}, "categories": {cf.ID}, "save": {"Save"},
	})
	redirectedTo(t, resp, "/admin/pages?saved=1")
	page, err := h.store.GetPageByAlias(ctx, "About-This-Blog")
	if err != nil {
		t.Fatalf("the page was not created with an alias from its title: %v", err)
	}
	if !page.ShowLayout {
		t.Error("the showlayout checkbox was ticked but the page was saved without the layout")
	}
	if len(page.CategoryIDs) != 1 || page.CategoryIDs[0] != cf.ID {
		t.Errorf("the page's categories are %v, want just %s", page.CategoryIDs, cf.ID)
	}
	if *flushes != 1 {
		t.Errorf("the flush hook ran %d times on a save, want 1", *flushes)
	}

	// The list: the title links to the editor, with the alias, the layout
	// flag, the public URL and a delete form per row.
	resp, body := h.get("/admin/pages")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pages: status %d, want 200", resp.StatusCode)
	}
	for _, want := range []string{
		"<h1>Pages</h1>",
		`<a href="/admin/pages/` + page.ID + `">About This Blog</a>`,
		"<td>About-This-Blog</td>",
		"http://127.0.0.1:8080/page/About-This-Blog",
		"<td>Yes</td>",
		`href="/admin/pages/new"`,
		`action="/admin/pages/` + page.ID + `/delete"`,
		`value="Delete"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the pages list is missing %s", want)
		}
	}

	// A reserved first segment is refused, as it is for a category.
	resp, body = h.postForm("/admin/pages/new", url.Values{
		"title": {"Search"}, "alias": {"search"}, "body": {"b"}, "save": {"Save"},
	})
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "a path the blog itself uses") {
		t.Errorf("the reserved alias `search`: status %d, want the form back with a message", resp.StatusCode)
	}
	if _, err := h.store.GetPageByAlias(ctx, "search"); !errors.Is(err, store.ErrNotFound) {
		t.Error("a page was created with the reserved alias `search`")
	}

	// A title or body left blank is refused, and so is a taken alias.
	resp, body = h.postForm("/admin/pages/new", url.Values{
		"title": {""}, "alias": {"nothing"}, "body": {""}, "save": {"Save"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("a blank page: status %d, want the form back", resp.StatusCode)
	}
	for _, want := range []string{"The title cannot be blank.", "The body cannot be blank."} {
		if !strings.Contains(body, want) {
			t.Errorf("a blank page does not say %q", want)
		}
	}
	resp, body = h.postForm("/admin/pages/new", url.Values{
		"title": {"Another"}, "alias": {"About-This-Blog"}, "body": {"b"}, "save": {"Save"},
	})
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "already exists") {
		t.Errorf("a taken alias: status %d, want the form back with a message", resp.StatusCode)
	}

	// The editor shows what was saved, with the page's categories ticked.
	_, body = h.get("/admin/pages/" + page.ID)
	for _, want := range []string{
		`value="About This Blog"`, `value="About-This-Blog"`, "Hello there.",
		`name="showlayout" value="1" checked`,
		`<option value="` + cf.ID + `" selected>ColdFusion</option>`,
		`<option value="` + other.ID + `">Go</option>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page editor is missing %s", want)
		}
	}

	// Editing: the layout comes off when the box is not ticked and the
	// category set is replaced by what the form carried.
	resp, _ = h.postForm("/admin/pages/"+page.ID, url.Values{
		"title": {"About"}, "alias": {"about"}, "body": {"Now with less layout."},
		"categories": {other.ID}, "save": {"Save"},
	})
	redirectedTo(t, resp, "/admin/pages?saved=1")
	page, err = h.store.GetPage(ctx, page.ID)
	if err != nil {
		t.Fatalf("GetPage after the edit: %v", err)
	}
	if page.Title != "About" || page.Alias != "about" || page.Body != "Now with less layout." {
		t.Errorf("the edit saved %+v", page)
	}
	if page.ShowLayout {
		t.Error("an unticked showlayout box left the layout on")
	}
	if len(page.CategoryIDs) != 1 || page.CategoryIDs[0] != other.ID {
		t.Errorf("the page's categories are %v, want just %s", page.CategoryIDs, other.ID)
	}

	// Delete.
	resp, _ = h.postForm("/admin/pages/"+page.ID+"/delete", url.Values{"delete": {"Delete"}})
	redirectedTo(t, resp, "/admin/pages?deleted=1")
	if _, err := h.store.GetPage(ctx, page.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetPage after the delete: %v, want ErrNotFound", err)
	}
	if *flushes < 3 {
		t.Errorf("the flush hook ran %d times, want one per write", *flushes)
	}
}
