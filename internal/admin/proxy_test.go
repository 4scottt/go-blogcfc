package admin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/4scottt/go-blogcfc/internal/store"
)

// TestFP_A09_RelatedEntriesProxyJSONAndSave covers PLAN §9 A09
// (admin/entry.cfm and admin/proxy.cfm): the picker's filter answers
// valid JSON for a text or a category, leaves the entry being edited
// out of it, and the ids the editor submits are stored delete-then-
// insert, in both directions, as SetRelatedEntries does.
func TestFP_A09_RelatedEntriesProxyJSONAndSave(t *testing.T) {
	h := newHarness(t)
	h.user("admin", "Admin")
	ctx := context.Background()
	now := time.Now().UTC()

	cf := h.createCategory("ColdFusion", "coldfusion")
	golang := h.createCategory("Go", "golang")
	subject := h.createEntry(&store.Entry{Title: "The subject entry", Alias: "subject", Body: "b",
		Posted: now.Add(-4 * time.Hour), Username: "admin", Released: true})
	needle := h.createEntry(&store.Entry{Title: "A needle entry", Alias: "needle", Body: "b",
		Posted: now.Add(-3 * time.Hour), Username: "admin", Released: true})
	draft := h.createEntry(&store.Entry{Title: "A draft entry", Alias: "draft-entry", Body: "b",
		Posted: now.Add(-2 * time.Hour), Username: "admin"})
	other := h.createEntry(&store.Entry{Title: "Something else", Alias: "else", Body: "b",
		Posted: now.Add(-time.Hour), Username: "admin", Released: true})
	if err := h.store.SetEntryCategories(ctx, needle.ID, []string{cf.ID}); err != nil {
		t.Fatalf("SetEntryCategories: %v", err)
	}
	if err := h.store.SetEntryCategories(ctx, other.ID, []string{golang.ID}); err != nil {
		t.Fatalf("SetEntryCategories: %v", err)
	}

	h.login("admin")

	// The filter form and the picker's controls are on the editor.
	_, body := h.get("/admin/entries/" + subject.ID)
	for _, want := range []string{`name="relatedtext"`, `name="relatedcategory"`, `name="related"`, "/admin/proxy"} {
		if !strings.Contains(body, want) {
			t.Errorf("the Related Entries tab has no %s", want)
		}
	}

	proxy := func(query string) []map[string]string {
		t.Helper()
		resp, raw := h.get("/admin/proxy?" + query)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /admin/proxy?%s: status %d", query, resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("proxy Content-Type = %q, want application/json", ct)
		}
		var out []map[string]string
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			t.Fatalf("the proxy did not answer valid JSON (%v): %s", err, raw)
		}
		return out
	}

	byText := proxy("text=needle&entry=" + url.QueryEscape(subject.ID))
	if len(byText) != 1 || byText[0]["id"] != needle.ID || byText[0]["title"] != needle.Title {
		t.Fatalf("filtering by text gave %+v, want the one needle entry", byText)
	}

	byCat := proxy("category=" + url.QueryEscape(golang.ID) + "&entry=" + url.QueryEscape(subject.ID))
	if len(byCat) != 1 || byCat[0]["id"] != other.ID {
		t.Fatalf("filtering by category gave %+v, want the Go entry", byCat)
	}

	// Drafts are offered: the author is picking from their own blog.
	all := proxy("text=entry&entry=" + url.QueryEscape(subject.ID))
	if len(all) != 2 {
		t.Errorf("filtering gave %d entries, want the needle and the draft", len(all))
	}
	sawDraft := false
	for _, row := range all {
		if row["id"] == draft.ID {
			sawDraft = true
		}
	}
	if !sawDraft {
		t.Error("the picker does not offer a draft entry")
	}

	// The entry being edited is never in its own list.
	for _, row := range proxy("text=subject&entry=" + url.QueryEscape(subject.ID)) {
		if row["id"] == subject.ID {
			t.Error("the proxy offered the entry being edited as a relation of itself")
		}
	}

	// With no filter at all the answer is an empty array, not a page of
	// entries and not a fault.
	if none := proxy(""); len(none) != 0 {
		t.Errorf("an unfiltered proxy call returned %d rows, want none", len(none))
	}

	// Saving the picked set.
	form := entryFormValues("The subject entry", "subject")
	form["related"] = []string{needle.ID, draft.ID}
	resp, _ := h.postForm("/admin/entries/"+subject.ID, form)
	redirectedTo(t, resp, "/admin/entries?saved=1")

	related, err := h.store.RelatedEntriesForEditor(ctx, subject.ID)
	if err != nil {
		t.Fatalf("RelatedEntriesForEditor: %v", err)
	}
	if len(related) != 2 {
		t.Fatalf("the entry has %d relations, want two", len(related))
	}
	// The relation reads back from the other side too (P13's union).
	back, err := h.store.RelatedEntriesForEditor(ctx, needle.ID)
	if err != nil {
		t.Fatalf("RelatedEntriesForEditor(needle): %v", err)
	}
	if len(back) != 1 || back[0].ID != subject.ID {
		t.Errorf("the related entry does not point back: %+v", back)
	}

	// The editor shows them, selected, so a plain save keeps them.
	_, body = h.get("/admin/entries/" + subject.ID)
	if !strings.Contains(body, `<option value="`+needle.ID+`" selected>`+needle.Title) {
		t.Error("the picker does not show the saved relation as a chosen option")
	}

	// Saving a smaller set deletes what is gone and keeps the rest.
	form = entryFormValues("The subject entry", "subject")
	form["related"] = []string{draft.ID, "not-an-entry", subject.ID}
	resp, _ = h.postForm("/admin/entries/"+subject.ID, form)
	redirectedTo(t, resp, "/admin/entries?saved=1")
	related, err = h.store.RelatedEntriesForEditor(ctx, subject.ID)
	if err != nil {
		t.Fatalf("RelatedEntriesForEditor: %v", err)
	}
	if len(related) != 1 || related[0].ID != draft.ID {
		t.Errorf("after the second save the relations are %+v, want the draft only", related)
	}

	// An empty picker clears the set.
	resp, _ = h.postForm("/admin/entries/"+subject.ID, entryFormValues("The subject entry", "subject"))
	redirectedTo(t, resp, "/admin/entries?saved=1")
	if related, err = h.store.RelatedEntriesForEditor(ctx, subject.ID); err != nil || len(related) != 0 {
		t.Errorf("saving with no relations left %+v (err %v)", related, err)
	}
}

// TestFP_A09_ProxyNeedsASession: the picker's JSON is admin data and
// sits behind the same gate as the screen that fetches it.
func TestFP_A09_ProxyNeedsASession(t *testing.T) {
	h := newHarness(t)
	h.user("admin", "Admin")
	resp, _ := h.get("/admin/proxy?text=anything")
	if resp.StatusCode != http.StatusFound || !strings.HasPrefix(resp.Header.Get("Location"), "/admin/login") {
		t.Fatalf("signed out: status %d to %q, want the login", resp.StatusCode, resp.Header.Get("Location"))
	}
}
