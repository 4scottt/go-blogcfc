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

// createComment puts a comment in the database and returns it.
func (h *harness) createComment(c *store.Comment) *store.Comment {
	h.t.Helper()
	if err := h.store.CreateComment(context.Background(), c); err != nil {
		h.t.Fatalf("CreateComment(%s): %v", c.Name, err)
	}
	return c
}

// comment reads a comment back.
func (h *harness) comment(id string) *store.Comment {
	h.t.Helper()
	c, err := h.store.GetComment(context.Background(), id)
	if err != nil {
		h.t.Fatalf("GetComment(%s): %v", id, err)
	}
	return c
}

// commentGone fails unless the comment is no longer there.
func (h *harness) commentGone(id string) {
	h.t.Helper()
	if _, err := h.store.GetComment(context.Background(), id); !errors.Is(err, store.ErrNotFound) {
		h.t.Errorf("comment %s is still there (err %v)", id, err)
	}
}

// TestFP_A13_CommentsListSearchBulkDeleteAndEdit covers PLAN §9 A13: the
// list with its search over text and name, the 20-row page, the bulk
// delete, and the editor's five fields (admin/comments.cfm, comment.cfm).
func TestFP_A13_CommentsListSearchBulkDeleteAndEdit(t *testing.T) {
	h := newHarness(t)
	h.user("admin", "Admin")
	now := time.Now().UTC().Truncate(time.Second)

	entry := h.createEntry(&store.Entry{Title: "Hosting entry", Alias: "hosting", Body: "b",
		Posted: now.Add(-time.Hour), Username: "admin", Released: true, AllowComments: true})

	inText := h.createComment(&store.Comment{EntryID: entry.ID, Name: "Ada", Email: "ada@example.com",
		Comment: "a NEEDLE in the text", Posted: now.Add(-30 * time.Minute), Moderated: true})
	inName := h.createComment(&store.Comment{EntryID: entry.ID, Name: "Needle Jones", Email: "nj@example.com",
		Comment: "nothing to see", Posted: now.Add(-20 * time.Minute)})
	plain := h.createComment(&store.Comment{EntryID: entry.ID, Name: "Grace", Email: "grace@example.com",
		Comment: "unrelated", Posted: now.Add(-10 * time.Minute), Moderated: true})

	h.login("admin")
	resp, body := h.get("/admin/comments")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("comments: status %d, want 200", resp.StatusCode)
	}
	for _, want := range []string{
		"<h1>Comments</h1>",
		`name="search"`,
		`action="/admin/comments/delete"`,
		`value="Delete Marked"`,
		`<input type="checkbox" name="mark" value="` + plain.ID + `">`,
		`id="markall"`,
		`<a href="/admin/comments/` + inText.ID + `">`,
		"Hosting entry",
		"ada@example.com",
		"a NEEDLE in the text",
		"3 comments",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the comments list is missing %s", want)
		}
	}
	// The entry link is the public page with adminview and the comment anchor.
	if !strings.Contains(body, "/?mode=entry&amp;entry="+entry.ID+"&amp;adminview=1#c"+inText.ID) {
		t.Error("the list does not link the entry title to the comment on the public page")
	}
	// The moderated column shows both states, so a held comment is reachable
	// from this screen (the as-is hid them once moderation was on).
	if !strings.Contains(body, "<td>Yes</td>") || !strings.Contains(body, "<td>No</td>") {
		t.Error("the list does not show the moderated flag both ways")
	}

	// The search matches the comment text and the commenter's name.
	_, body = h.get("/admin/comments?search=needle")
	if !strings.Contains(body, inText.Name) || !strings.Contains(body, inName.Name) {
		t.Error("the search missed the text match or the name match")
	}
	if strings.Contains(body, plain.Name) {
		t.Error("the search returned a comment that matches neither text nor name")
	}
	if !strings.Contains(body, "Your filtered search returned") || !strings.Contains(body, "2 comments") {
		t.Error("the filtered list does not say how many it found")
	}

	// Bulk delete.
	resp, _ = h.postForm("/admin/comments/delete", url.Values{"mark": {plain.ID}, "delete": {"Delete Marked"}})
	loc := redirectedTo(t, resp, "/admin/comments?deleted=1")
	h.commentGone(plain.ID)
	_, body = h.get(loc)
	if !strings.Contains(body, "1 comment deleted.") {
		t.Error("the list does not report the delete")
	}

	// The editor carries every field the plan names, by name.
	resp, body = h.get("/admin/comments/" + inName.ID)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("comment editor: status %d, want 200", resp.StatusCode)
	}
	for _, want := range []string{
		"<h1>Comment Editor</h1>",
		`name="name"`, `name="email"`, `name="website"`, `name="comment"`,
		`name="subscribe"`, `name="moderated"`,
		`value="Save"`, `name="approve"`, `value="Approve"`,
		`action="/admin/comments/` + inName.ID + `"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the comment editor is missing %s", want)
		}
	}

	// A refused save says why and keeps what was typed.
	form := url.Values{
		"name": {"Needle Jones"}, "email": {"not-an-address"}, "website": {"example.com"},
		"comment": {"edited text"}, "subscribe": {"yes"}, "moderated": {"yes"}, "save": {"Save"},
	}
	resp, body = h.postForm("/admin/comments/"+inName.ID, form)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bad save: status %d, want 200", resp.StatusCode)
	}
	for _, want := range []string{
		"The email cannot be blank and must be a valid email address.",
		"Website must be a valid URL.",
		"edited text",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the refused save does not show %q", want)
		}
	}
	if c := h.comment(inName.ID); c.Comment != "nothing to see" {
		t.Error("a refused save wrote to the database anyway")
	}

	form.Set("email", "needle@example.com")
	form.Set("website", "https://example.com/blog")
	resp, _ = h.postForm("/admin/comments/"+inName.ID, form)
	redirectedTo(t, resp, "/admin/comments?saved=1")
	saved := h.comment(inName.ID)
	if saved.Email != "needle@example.com" || saved.Website != "https://example.com/blog" ||
		saved.Comment != "edited text" || !saved.Subscribe || !saved.Moderated {
		t.Errorf("the saved comment is %+v, want the edited fields with subscribe and moderated on", saved)
	}

	// Twenty more comments make a second page.
	for i := 0; i < 20; i++ {
		h.createComment(&store.Comment{EntryID: entry.ID, Name: "Bulk", Email: "bulk@example.com",
			Comment: "filler", Posted: now.Add(-time.Duration(i) * time.Second), Moderated: true})
	}
	_, body = h.get("/admin/comments")
	if !strings.Contains(body, "Page 1 of 2") {
		t.Error("the comments list does not page at 20")
	}
	if n := strings.Count(body, `name="mark" value="`); n != 20 {
		t.Errorf("page 1 has %d rows, want 20", n)
	}
	if !strings.Contains(body, `id="nextpage"`) {
		t.Error("the comments list has no next-page link")
	}
	_, body = h.get("/admin/comments?page=2")
	if n := strings.Count(body, `name="mark" value="`); n != 2 {
		t.Errorf("page 2 has %d rows, want the remaining 2", n)
	}
}
