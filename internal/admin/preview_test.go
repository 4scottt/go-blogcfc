package admin_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/4scottt/go-blogcfc/internal/store"
)

// TestFP_A10_PreviewRendersWithoutSaving covers PLAN §9 A10
// (admin/entry.cfm's preview branch): the Preview button renders the
// entry as a reader would meet it, inside the admin frame, and writes
// nothing; Return and Save carry every typed value back.
func TestFP_A10_PreviewRendersWithoutSaving(t *testing.T) {
	h := newHarness(t)
	h.user("admin", "Admin")
	ctx := context.Background()

	cat := h.createCategory("ColdFusion", "coldfusion")
	e := h.createEntry(&store.Entry{Title: "As stored", Alias: "as-stored", Body: "The stored body",
		Posted: time.Now().UTC().Add(-time.Hour), Username: "admin", Released: true})
	if err := h.store.SetEntryCategories(ctx, e.ID, []string{cat.ID}); err != nil {
		t.Fatalf("SetEntryCategories: %v", err)
	}
	if err := h.store.CreateTextblock(ctx, &store.Textblock{Label: "signoff", Body: "Written by hand."}); err != nil {
		t.Fatalf("CreateTextblock: %v", err)
	}

	h.login("admin")

	// The button is on the editor, named and labelled as the Pilot and
	// the walk expect.
	_, editor := h.get("/admin/entries/" + e.ID)
	if !strings.Contains(editor, `name="preview" value="Preview"`) {
		t.Error("the editor has no Preview button")
	}

	form := entryFormValues("A new title", "as-stored")
	form.Set("body", "First paragraph\n\nSecond paragraph<more/>The rest of it <textblock label=\"signoff\">")
	form.Set("categories", cat.ID)
	form.Del("save")
	form.Set("preview", "Preview")
	resp, body := h.postForm("/admin/entries/"+e.ID, form)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preview: status %d, want 200", resp.StatusCode)
	}

	for _, want := range []string{
		"A new title",
		"<p>First paragraph</p>",
		"Second paragraph",
		"The rest of it",
		"Written by hand.",
		"ColdFusion",
		`name="return" value="Return"`,
		`name="save" value="Save"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the preview does not contain %q", want)
		}
	}
	if strings.Contains(body, "<more/>") {
		t.Error("the preview shows the <more/> tag instead of splitting on it")
	}

	// Nothing was written.
	stored := h.entryByAlias("as-stored")
	if stored.Title != "As stored" || stored.Body != "The stored body" {
		t.Errorf("the preview saved the entry: %q / %q", stored.Title, stored.Body)
	}

	// Everything typed comes back in the preview's own form, so Return
	// and Save start where the author left off.
	for _, want := range []string{
		`name="title" value="A new title"`,
		`name="body"`,
		`name="categories" value="` + cat.ID + `"`,
		`name="alias" value="as-stored"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the preview's form does not carry %q", want)
		}
	}

	// Return is the editor again, still unsaved.
	form.Del("preview")
	form.Set("return", "Return")
	resp, body = h.postForm("/admin/entries/"+e.ID, form)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("return: status %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, `id="editForm"`) || !strings.Contains(body, `value="A new title"`) {
		t.Error("Return did not come back to the editor with the typed values")
	}
	if stored := h.entryByAlias("as-stored"); stored.Title != "As stored" {
		t.Error("Return saved the entry")
	}

	// Save from the preview's form does save.
	form.Del("return")
	form.Set("save", "Save")
	resp, _ = h.postForm("/admin/entries/"+e.ID, form)
	redirectedTo(t, resp, "/admin/entries?saved=1")
	saved := h.entryByAlias("as-stored")
	if saved.Title != "A new title" || saved.MoreBody == "" {
		t.Errorf("Save from the preview stored %q with morebody %q", saved.Title, saved.MoreBody)
	}

	// A new entry previews too, and is not created by it.
	form = entryFormValues("Never saved", "never-saved")
	form.Del("save")
	form.Set("preview", "Preview")
	resp, body = h.postForm("/admin/entries/new", form)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "Never saved") {
		t.Fatalf("previewing a new entry: status %d", resp.StatusCode)
	}
	if _, err := h.store.GetEntryByAlias(ctx, "never-saved"); err == nil {
		t.Error("previewing a new entry created it")
	}
}
