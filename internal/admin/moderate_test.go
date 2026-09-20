package admin_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/4scottt/go-blogcfc/internal/store"
)

// notifyCall is one call of the admin's notification hook.
type notifyCall struct {
	EntryID   string
	CommentID string
	AdminOnly bool
}

// notifyRecorder stands in for the notification package: it records what
// the admin asked for and sends nothing.
type notifyRecorder struct {
	mu    sync.Mutex
	calls []notifyCall
}

func (n *notifyRecorder) hook() func(context.Context, *store.Entry, *store.Comment, bool) (int, error) {
	return func(_ context.Context, e *store.Entry, c *store.Comment, adminOnly bool) (int, error) {
		n.mu.Lock()
		defer n.mu.Unlock()
		n.calls = append(n.calls, notifyCall{EntryID: e.ID, CommentID: c.ID, AdminOnly: adminOnly})
		return 1, nil
	}
}

func (n *notifyRecorder) all() []notifyCall {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]notifyCall(nil), n.calls...)
}

// TestFP_A14_ModerationQueueApproveBulkDeleteAndMenuCount covers PLAN §9
// A14: the queue of held comments, the Approve link, the bulk delete and
// the live count in the left menu (admin/moderate.cfm, the as-is menu's
// getNumberUnmoderated).
func TestFP_A14_ModerationQueueApproveBulkDeleteAndMenuCount(t *testing.T) {
	h := newHarness(t)
	h.user("admin", "Admin")
	now := time.Now().UTC().Truncate(time.Second)

	entry := h.createEntry(&store.Entry{Title: "Queue entry", Alias: "queue", Body: "b",
		Posted: now.Add(-time.Hour), Username: "admin", Released: true, AllowComments: true})

	done := h.createComment(&store.Comment{EntryID: entry.ID, Name: "Approved already",
		Email: "done@example.com", Comment: "fine", Posted: now.Add(-40 * time.Minute), Moderated: true})
	first := h.createComment(&store.Comment{EntryID: entry.ID, Name: "Waiting one",
		Email: "one@example.com", Comment: "hold me", Posted: now.Add(-30 * time.Minute)})
	second := h.createComment(&store.Comment{EntryID: entry.ID, Name: "Waiting two",
		Email: "two@example.com", Comment: "hold me too", Posted: now.Add(-20 * time.Minute)})

	h.login("admin")

	// The menu on any admin page carries the count.
	_, body := h.get("/admin/entries")
	if !strings.Contains(body, "Moderate (2)") {
		t.Error("the menu does not show the moderation count")
	}

	resp, body := h.get("/admin/moderate")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("moderate: status %d, want 200", resp.StatusCode)
	}
	for _, want := range []string{
		"<h1>Moderate Comments</h1>",
		"2 comments to moderate",
		`href="/admin/moderate?approve=` + first.ID + `"`,
		`<input type="checkbox" name="mark" value="` + second.ID + `">`,
		`action="/admin/moderate/delete"`,
		`value="Delete Marked"`,
		"Waiting one", "Waiting two", "Queue entry",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the moderation queue is missing %s", want)
		}
	}
	if strings.Contains(body, done.Name) {
		t.Error("an approved comment is in the moderation queue")
	}

	// Approving redirects, so a reload does not approve twice.
	resp, _ = h.get("/admin/moderate?approve=" + first.ID)
	loc := redirectedTo(t, resp, "/admin/moderate?approved=1")
	if c := h.comment(first.ID); !c.Moderated {
		t.Error("the Approve link did not moderate the comment")
	}
	_, body = h.get(loc)
	if !strings.Contains(body, "Comment approved.") {
		t.Error("the queue does not report the approval")
	}
	if !strings.Contains(body, "Moderate (1)") {
		t.Error("the menu count did not fall after an approval")
	}
	if strings.Contains(body, "Waiting one") {
		t.Error("an approved comment is still in the queue")
	}

	// Bulk delete from the queue comes back to the queue.
	resp, _ = h.postForm("/admin/moderate/delete", url.Values{"mark": {second.ID}, "delete": {"Delete Marked"}})
	loc = redirectedTo(t, resp, "/admin/moderate?deleted=1")
	h.commentGone(second.ID)
	_, body = h.get(loc)
	if !strings.Contains(body, "Moderate (0)") {
		t.Error("the menu count did not fall after a delete")
	}
	if !strings.Contains(body, "Nothing is waiting to be moderated.") {
		t.Error("the empty queue has no empty state")
	}

	// An unknown id is a 404, not a silent redirect.
	resp, _ = h.get("/admin/moderate?approve=00000000-0000-4000-8000-000000000000")
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("approving an unknown comment: status %d, want 404", resp.StatusCode)
	}
}

// TestFP_C12_ApprovingRenotifiesSubscribersNotAdmin covers PLAN §9 C12
// and §11 "Notification recipients": approving a held comment, from the
// queue or from the editor's Approve button, re-notifies the thread's
// subscribers — the notification goes out with adminOnly false, which is
// the subscribers' copy, not the owner-only one a held comment got.
func TestFP_C12_ApprovingRenotifiesSubscribersNotAdmin(t *testing.T) {
	h := newHarness(t)
	h.user("admin", "Admin")
	now := time.Now().UTC().Truncate(time.Second)

	notifies := &notifyRecorder{}
	h.module.Notify = notifies.hook()

	entry := h.createEntry(&store.Entry{Title: "Watched entry", Alias: "watched", Body: "b",
		Posted: now.Add(-time.Hour), Username: "admin", Released: true, AllowComments: true})
	// Someone is watching the thread, which is who a re-notify reaches.
	h.createComment(&store.Comment{EntryID: entry.ID, Name: "Watcher", Email: "watcher@example.com",
		Posted: now.Add(-50 * time.Minute), Moderated: true, Subscribe: true, SubscribeOnly: true})

	fromQueue := h.createComment(&store.Comment{EntryID: entry.ID, Name: "From the queue",
		Email: "q@example.com", Comment: "held one", Posted: now.Add(-30 * time.Minute)})
	fromEditor := h.createComment(&store.Comment{EntryID: entry.ID, Name: "From the editor",
		Email: "e@example.com", Comment: "held two", Posted: now.Add(-20 * time.Minute)})

	h.login("admin")

	resp, _ := h.get("/admin/moderate?approve=" + fromQueue.ID)
	redirectedTo(t, resp, "/admin/moderate?approved=1")

	resp, _ = h.postForm("/admin/comments/"+fromEditor.ID, url.Values{
		"name": {"From the editor"}, "email": {"e@example.com"}, "website": {""},
		"comment": {"held two"}, "subscribe": {"no"}, "moderated": {"no"}, "approve": {"Approve"},
	})
	redirectedTo(t, resp, "/admin/comments?approved=1")

	calls := notifies.all()
	if len(calls) != 2 {
		t.Fatalf("the notification hook ran %d times, want one per approval: %+v", len(calls), calls)
	}
	for i, want := range []string{fromQueue.ID, fromEditor.ID} {
		if calls[i].CommentID != want {
			t.Errorf("call %d was for comment %s, want %s", i, calls[i].CommentID, want)
		}
		if calls[i].EntryID != entry.ID {
			t.Errorf("call %d carried entry %s, want %s", i, calls[i].EntryID, entry.ID)
		}
		if calls[i].AdminOnly {
			t.Errorf("call %d went out adminOnly: approving notifies the subscribers, not the admin alone", i)
		}
	}
	// Both comments are moderated now, the editor's despite its form
	// saying moderated=no: Approve does not save the fields.
	if c := h.comment(fromQueue.ID); !c.Moderated {
		t.Error("the queue's Approve did not moderate the comment")
	}
	if c := h.comment(fromEditor.ID); !c.Moderated {
		t.Error("the editor's Approve did not moderate the comment")
	}

	// A plain Save notifies nobody.
	resp, _ = h.postForm("/admin/comments/"+fromEditor.ID, url.Values{
		"name": {"From the editor"}, "email": {"e@example.com"}, "website": {""},
		"comment": {"edited again"}, "subscribe": {"no"}, "moderated": {"yes"}, "save": {"Save"},
	})
	redirectedTo(t, resp, "/admin/comments?saved=1")
	if n := len(notifies.all()); n != 2 {
		t.Errorf("a Save sent a notification: the hook has run %d times, want 2", n)
	}
}
