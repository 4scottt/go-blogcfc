package admin_test

import (
	"context"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/4scottt/go-blogcfc/internal/store"
)

// releaseCall is one call of the admin's release hook.
type releaseCall struct {
	EntryID        string
	Released       bool
	ReleasedBefore bool
}

// TestEntrySaveCallsReleaseHookWithReleasedBefore: the editor tells the
// release package what the entry's stored `released` flag was before the
// save, which is how a first release is told from a re-save (PLAN §11
// "Release side effects"). A new entry was never released.
func TestEntrySaveCallsReleaseHookWithReleasedBefore(t *testing.T) {
	h := newHarness(t)
	h.user("admin", "Admin")
	now := time.Now().UTC().Truncate(time.Second)

	var mu sync.Mutex
	var calls []releaseCall
	h.module.Release = func(_ context.Context, e *store.Entry, releasedBefore bool) error {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, releaseCall{EntryID: e.ID, Released: e.Released, ReleasedBefore: releasedBefore})
		return nil
	}
	at := func(i int) releaseCall {
		mu.Lock()
		defer mu.Unlock()
		if i >= len(calls) {
			t.Fatalf("the release hook ran %d times, want at least %d", len(calls), i+1)
		}
		return calls[i]
	}

	draft := h.createEntry(&store.Entry{Title: "A draft", Alias: "a-draft", Body: "b",
		Posted: now.Add(-time.Hour), Username: "admin"})
	live := h.createEntry(&store.Entry{Title: "Already out", Alias: "already-out", Body: "b",
		Posted: now.Add(-time.Hour), Username: "admin", Released: true})

	h.login("admin")
	form := func(title, alias string, released bool) url.Values {
		v := url.Values{"title": {title}, "body": {"b"}, "alias": {alias},
			"posted": {now.Add(-time.Hour).Format("2006-01-02 15:04")}, "save": {"Save"}}
		if released {
			v.Set("released", "on")
		}
		return v
	}

	// A new entry: nothing was released before, whatever it is saved as.
	resp, _ := h.postForm("/admin/entries/new", form("Brand new", "brand-new", true))
	redirectedTo(t, resp, "/admin/entries?saved=1")
	first := at(0)
	if first.ReleasedBefore {
		t.Error("a new entry was reported as released before the save")
	}
	if !first.Released || first.EntryID == "" {
		t.Errorf("the hook got %+v, want the saved, released entry", first)
	}

	// An existing draft going out: released before is still false.
	resp, _ = h.postForm("/admin/entries/"+draft.ID, form("A draft", "a-draft", true))
	redirectedTo(t, resp, "/admin/entries?saved=1")
	second := at(1)
	if second.ReleasedBefore {
		t.Error("a draft was reported as released before the save")
	}
	if second.EntryID != draft.ID || !second.Released {
		t.Errorf("the hook got %+v, want the draft, now released", second)
	}

	// An entry that was already out: released before is true, so the
	// release package knows not to mail it again.
	resp, _ = h.postForm("/admin/entries/"+live.ID, form("Already out", "already-out", true))
	redirectedTo(t, resp, "/admin/entries?saved=1")
	third := at(2)
	if !third.ReleasedBefore {
		t.Error("a released entry was not reported as released before the save")
	}
	if third.EntryID != live.ID {
		t.Errorf("the hook got entry %s, want %s", third.EntryID, live.ID)
	}

	mu.Lock()
	n := len(calls)
	mu.Unlock()
	if n != 3 {
		t.Errorf("the release hook ran %d times, want once per save", n)
	}
}
