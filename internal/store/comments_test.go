package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/4scottt/go-blogcfc/internal/store"
	"github.com/4scottt/go-blogcfc/internal/testdb"
)

func mustCreateComment(t *testing.T, st *store.Store, c *store.Comment) *store.Comment {
	t.Helper()
	if err := st.CreateComment(context.Background(), c); err != nil {
		t.Fatalf("CreateComment(%s): %v", c.Name, err)
	}
	return c
}

// TestCommentsListingHidesUnmoderatedAndSubscriptions is the public
// entry's view of a thread: moderated comments oldest first, no
// subscribe-only rows, counts that agree with the list.
func TestCommentsListingHidesUnmoderatedAndSubscriptions(t *testing.T) {
	st := testdb.New(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	entry := mustCreateEntry(t, st, &store.Entry{Title: "Entry", Alias: "entry", Body: "b",
		Posted: now.Add(-time.Hour), Username: "admin", Released: true, AllowComments: true})
	other := mustCreateEntry(t, st, &store.Entry{Title: "Other", Alias: "other", Body: "b",
		Posted: now.Add(-time.Hour), Username: "admin", Released: true, AllowComments: true})

	second := mustCreateComment(t, st, &store.Comment{EntryID: entry.ID, Name: "Second",
		Email: "b@example.com", Comment: "later", Posted: now.Add(-10 * time.Minute), Moderated: true})
	first := mustCreateComment(t, st, &store.Comment{EntryID: entry.ID, Name: "First",
		Email: "a@example.com", Comment: "earlier", Posted: now.Add(-30 * time.Minute), Moderated: true})
	held := mustCreateComment(t, st, &store.Comment{EntryID: entry.ID, Name: "Held",
		Email: "c@example.com", Comment: "in the queue", Posted: now.Add(-5 * time.Minute)})
	mustCreateComment(t, st, &store.Comment{EntryID: entry.ID, Name: "Watcher",
		Email: "w@example.com", Posted: now.Add(-time.Minute), Moderated: true,
		Subscribe: true, SubscribeOnly: true})
	mustCreateComment(t, st, &store.Comment{EntryID: other.ID, Name: "Elsewhere",
		Email: "e@example.com", Comment: "other thread", Posted: now, Moderated: true})

	if first.ID == "" || first.KillToken == "" {
		t.Fatal("CreateComment filled neither the id nor the kill token")
	}
	if first.KillToken == second.KillToken {
		t.Error("two comments share a kill token")
	}

	public, err := st.ListComments(ctx, entry.ID, false)
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if len(public) != 2 || public[0].ID != first.ID || public[1].ID != second.ID {
		t.Fatalf("public list = %+v, want the two moderated comments oldest first", public)
	}
	if public[0].Comment != "earlier" || !public[0].Posted.Equal(now.Add(-30*time.Minute)) {
		t.Errorf("first comment round-tripped as %+v", public[0])
	}

	withHeld, err := st.ListComments(ctx, entry.ID, true)
	if err != nil {
		t.Fatalf("ListComments(includeUnmoderated): %v", err)
	}
	if len(withHeld) != 3 || withHeld[2].ID != held.ID {
		t.Fatalf("admin list = %d rows, want 3 ending in the held comment", len(withHeld))
	}

	n, err := st.CountComments(ctx, entry.ID)
	if err != nil || n != 2 {
		t.Fatalf("CountComments = %d (%v), want 2", n, err)
	}
	counts, err := st.CountCommentsFor(ctx, []string{entry.ID, other.ID, "no-such-entry"})
	if err != nil {
		t.Fatalf("CountCommentsFor: %v", err)
	}
	if counts[entry.ID] != 2 || counts[other.ID] != 1 || counts["no-such-entry"] != 0 {
		t.Fatalf("CountCommentsFor = %v", counts)
	}
	if empty, err := st.CountCommentsFor(ctx, nil); err != nil || len(empty) != 0 {
		t.Fatalf("CountCommentsFor(nil) = %v (%v)", empty, err)
	}
}

// TestRecentCommentsCarryTheirEntryAndSkipDrafts: the sidebar pod never
// leaks a draft's thread.
func TestRecentCommentsCarryTheirEntryAndSkipDrafts(t *testing.T) {
	st := testdb.New(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	live := mustCreateEntry(t, st, &store.Entry{Title: "Live entry", Alias: "live", Body: "b",
		Posted: now.Add(-time.Hour), Username: "admin", Released: true, AllowComments: true})
	draft := mustCreateEntry(t, st, &store.Entry{Title: "Draft entry", Alias: "draft", Body: "b",
		Posted: now.Add(-time.Hour), Username: "admin", AllowComments: true})

	newest := mustCreateComment(t, st, &store.Comment{EntryID: live.ID, Name: "Newest",
		Comment: "newest", Posted: now.Add(-time.Minute), Moderated: true})
	mustCreateComment(t, st, &store.Comment{EntryID: live.ID, Name: "Older",
		Comment: "older", Posted: now.Add(-time.Hour), Moderated: true})
	mustCreateComment(t, st, &store.Comment{EntryID: live.ID, Name: "Held",
		Comment: "held", Posted: now})
	mustCreateComment(t, st, &store.Comment{EntryID: draft.ID, Name: "Hidden",
		Comment: "on a draft", Posted: now, Moderated: true})

	recent, err := st.RecentComments(ctx, 5)
	if err != nil {
		t.Fatalf("RecentComments: %v", err)
	}
	if len(recent) != 2 {
		t.Fatalf("RecentComments = %d rows, want 2 (the draft's and the held one stay out)", len(recent))
	}
	if recent[0].ID != newest.ID || recent[0].EntryTitle != "Live entry" || recent[0].EntryID != live.ID {
		t.Fatalf("newest recent comment = %+v", recent[0])
	}

	capped, err := st.RecentComments(ctx, 1)
	if err != nil || len(capped) != 1 {
		t.Fatalf("RecentComments(1) = %d rows (%v)", len(capped), err)
	}
}

// TestCommentModerationKillTokensAndSubscriptions covers the pipeline's
// store side: approve, the one-click kill link, the retro-clear of
// subscriptions and the notification map.
func TestCommentModerationKillTokensAndSubscriptions(t *testing.T) {
	st := testdb.New(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	entry := mustCreateEntry(t, st, &store.Entry{Title: "Entry", Alias: "entry", Body: "b",
		Posted: now.Add(-time.Hour), Username: "admin", Released: true, AllowComments: true})

	held := mustCreateComment(t, st, &store.Comment{EntryID: entry.ID, Name: "Held",
		Email: "held@example.com", Comment: "waiting", Posted: now.Add(-time.Minute)})
	if err := st.ApproveComment(ctx, held.ID); err != nil {
		t.Fatalf("ApproveComment: %v", err)
	}
	got, err := st.GetComment(ctx, held.ID)
	if err != nil || !got.Moderated {
		t.Fatalf("after approval: %+v (%v)", got, err)
	}
	if err := st.ApproveComment(ctx, "no-such-comment"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("approving a missing comment gave %v, want ErrNotFound", err)
	}

	byToken, err := st.GetCommentByKillToken(ctx, held.KillToken)
	if err != nil || byToken.ID != held.ID {
		t.Fatalf("GetCommentByKillToken: %+v (%v)", byToken, err)
	}
	if _, err := st.GetCommentByKillToken(ctx, ""); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("an empty kill token gave %v, want ErrNotFound", err)
	}
	if _, err := st.GetCommentByKillToken(ctx, "not-a-token"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("an unknown kill token gave %v, want ErrNotFound", err)
	}

	// Two comments by the same reader, both subscribed, plus a
	// subscribe-only watcher and someone who never subscribed.
	early := mustCreateComment(t, st, &store.Comment{EntryID: entry.ID, Name: "Reader",
		Email: "reader@example.com", Comment: "one", Posted: now.Add(-40 * time.Minute),
		Moderated: true, Subscribe: true})
	mustCreateComment(t, st, &store.Comment{EntryID: entry.ID, Name: "Reader",
		Email: "reader@example.com", Comment: "two", Posted: now.Add(-20 * time.Minute),
		Moderated: true, Subscribe: true})
	watcher := mustCreateComment(t, st, &store.Comment{EntryID: entry.ID, Name: "Watcher",
		Email: "watcher@example.com", Posted: now.Add(-30 * time.Minute),
		Moderated: true, Subscribe: true, SubscribeOnly: true})
	mustCreateComment(t, st, &store.Comment{EntryID: entry.ID, Name: "Passer-by",
		Email: "passer@example.com", Comment: "three", Posted: now, Moderated: true})

	subs, err := st.ThreadSubscribers(ctx, entry.ID)
	if err != nil {
		t.Fatalf("ThreadSubscribers: %v", err)
	}
	if len(subs) != 2 {
		t.Fatalf("ThreadSubscribers = %v, want the reader and the watcher", subs)
	}
	if subs["reader@example.com"] != early.ID {
		t.Errorf("the reader's unsubscribe link points at %q, want the earliest comment %q",
			subs["reader@example.com"], early.ID)
	}
	if subs["watcher@example.com"] != watcher.ID {
		t.Errorf("the subscribe-only watcher is missing from %v", subs)
	}

	// Commenting again with the box unticked clears the earlier ones.
	if err := st.ClearSubscriptions(ctx, entry.ID, "reader@example.com"); err != nil {
		t.Fatalf("ClearSubscriptions: %v", err)
	}
	subs, err = st.ThreadSubscribers(ctx, entry.ID)
	if err != nil {
		t.Fatalf("ThreadSubscribers: %v", err)
	}
	if _, still := subs["reader@example.com"]; still {
		t.Errorf("the reader is still subscribed after the retro-clear: %v", subs)
	}
	if len(subs) != 1 {
		t.Errorf("the watcher's subscription was cleared too: %v", subs)
	}
}

// TestCommentEditAndDelete is the admin comment screen's contract.
func TestCommentEditAndDelete(t *testing.T) {
	st := testdb.New(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	entry := mustCreateEntry(t, st, &store.Entry{Title: "Entry", Alias: "entry", Body: "b",
		Posted: now.Add(-time.Hour), Username: "admin", Released: true, AllowComments: true})
	c := mustCreateComment(t, st, &store.Comment{EntryID: entry.ID, Name: "Typo",
		Email: "typo@example.com", Website: "http://example.com", Comment: "teh", Posted: now, Moderated: true})

	c.Name, c.Comment, c.Subscribe = "Fixed", "the", true
	if err := st.UpdateComment(ctx, c); err != nil {
		t.Fatalf("UpdateComment: %v", err)
	}
	got, err := st.GetComment(ctx, c.ID)
	if err != nil {
		t.Fatalf("GetComment: %v", err)
	}
	if got.Name != "Fixed" || got.Comment != "the" || !got.Subscribe || got.KillToken != c.KillToken {
		t.Fatalf("edited comment = %+v", got)
	}
	if err := st.UpdateComment(ctx, &store.Comment{ID: "no-such-comment"}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("editing a missing comment gave %v, want ErrNotFound", err)
	}

	if err := st.DeleteComments(ctx, nil); err != nil {
		t.Fatalf("DeleteComments(nil): %v", err)
	}
	if err := st.DeleteComments(ctx, []string{c.ID}); err != nil {
		t.Fatalf("DeleteComments: %v", err)
	}
	if _, err := st.GetComment(ctx, c.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("the deleted comment gave %v, want ErrNotFound", err)
	}
}
