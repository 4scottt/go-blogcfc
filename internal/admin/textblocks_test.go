package admin_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestFP_A22_TextblocksCRUD covers PLAN §9 A22 (admin/textblocks.cfm,
// admin/textblock.cfm): the list with its delete form, the editor's two
// fields, a unique label, and the cache flush a saved block needs
// because an entry body that quotes it is cached elsewhere.
func TestFP_A22_TextblocksCRUD(t *testing.T) {
	h := newHarness(t)
	h.user("admin", "Admin")
	h.login("admin")
	ctx := context.Background()
	flushes := countFlushes(h)

	_, form := h.get("/admin/textblocks/new")
	for _, want := range []string{"<h1>Textblock Editor</h1>", `name="label"`, `name="body"`, `value="Save"`} {
		if !strings.Contains(form, want) {
			t.Errorf("the textblock editor has no %s", want)
		}
	}

	resp, _ := h.postForm("/admin/textblocks/new", url.Values{
		"label": {"footer"}, "body": {"Made in a rewrite."}, "save": {"Save"},
	})
	redirectedTo(t, resp, "/admin/textblocks?saved=1")
	tb, err := h.store.GetTextblockByLabel(ctx, "footer")
	if err != nil {
		t.Fatalf("the textblock was not created: %v", err)
	}
	if tb.Body != "Made in a rewrite." {
		t.Errorf("the textblock's body is %q", tb.Body)
	}
	if *flushes != 1 {
		t.Errorf("the flush hook ran %d times on a save, want 1", *flushes)
	}

	resp, body := h.get("/admin/textblocks")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("textblocks: status %d, want 200", resp.StatusCode)
	}
	for _, want := range []string{
		"<h1>Textblocks</h1>",
		`<a href="/admin/textblocks/` + tb.ID + `">footer</a>`,
		"Made in a rewrite.",
		`href="/admin/textblocks/new"`,
		`action="/admin/textblocks/` + tb.ID + `/delete"`,
		`value="Delete"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the textblocks list is missing %s", want)
		}
	}

	// A blank label or body is refused, and so is a label already taken.
	resp, body = h.postForm("/admin/textblocks/new", url.Values{"label": {""}, "body": {""}, "save": {"Save"}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("a blank textblock: status %d, want the form back", resp.StatusCode)
	}
	for _, want := range []string{"The label cannot be blank.", "The body cannot be blank."} {
		if !strings.Contains(body, want) {
			t.Errorf("a blank textblock does not say %q", want)
		}
	}
	resp, body = h.postForm("/admin/textblocks/new", url.Values{
		"label": {"footer"}, "body": {"another"}, "save": {"Save"},
	})
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "already exists") {
		t.Errorf("a taken label: status %d, want the form back with a message", resp.StatusCode)
	}
	if *flushes != 1 {
		t.Errorf("a refused save flushed the caches (%d flushes)", *flushes)
	}

	// The editor shows the block, and an edit writes both columns.
	_, body = h.get("/admin/textblocks/" + tb.ID)
	if !strings.Contains(body, `value="footer"`) || !strings.Contains(body, "Made in a rewrite.") {
		t.Error("the textblock editor does not show the stored block")
	}
	resp, _ = h.postForm("/admin/textblocks/"+tb.ID, url.Values{
		"label": {"footer-note"}, "body": {"Rewritten."}, "save": {"Save"},
	})
	redirectedTo(t, resp, "/admin/textblocks?saved=1")
	edited, err := h.store.GetTextblockByLabel(ctx, "footer-note")
	if err != nil {
		t.Fatalf("the edited textblock is not under its new label: %v", err)
	}
	if edited.ID != tb.ID || edited.Body != "Rewritten." {
		t.Errorf("the edit saved %+v", edited)
	}

	// An unknown id is a 404, not a 500.
	resp, _ = h.get("/admin/textblocks/00000000-0000-0000-0000-000000000000")
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("an unknown textblock: status %d, want 404", resp.StatusCode)
	}

	resp, _ = h.postForm("/admin/textblocks/"+tb.ID+"/delete", url.Values{"delete": {"Delete"}})
	redirectedTo(t, resp, "/admin/textblocks?deleted=1")
	blocks, err := h.store.ListTextblocks(ctx)
	if err != nil {
		t.Fatalf("ListTextblocks: %v", err)
	}
	if len(blocks) != 0 {
		t.Errorf("%d textblocks survived the delete", len(blocks))
	}
	if *flushes != 3 {
		t.Errorf("the flush hook ran %d times, want 3 (create, edit, delete)", *flushes)
	}
}
