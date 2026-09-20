package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/4scottt/go-blogcfc/internal/store"
	"github.com/4scottt/go-blogcfc/internal/testdb"
)

// TestSearchCommentsPagesAndMatchesTextOrName covers the admin comment
// list's query (PLAN §9 A13, admin/comments.cfm): newest first, the
// entry's title beside each row, a search over the comment text and the
// commenter's name, and paging with a total that ignores the page.
func TestSearchCommentsPagesAndMatchesTextOrName(t *testing.T) {
	st := testdb.New(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	entry := mustCreateEntry(t, st, &store.Entry{Title: "First entry", Alias: "first", Body: "b",
		Posted: now.Add(-time.Hour), Username: "admin", Released: true, AllowComments: true})
	other := mustCreateEntry(t, st, &store.Entry{Title: "Other entry", Alias: "other", Body: "b",
		Posted: now.Add(-time.Hour), Username: "admin", Released: true, AllowComments: true})

	oldest := mustCreateComment(t, st, &store.Comment{EntryID: entry.ID, Name: "Ada",
		Email: "ada@example.com", Comment: "a NEEDLE in the text", Posted: now.Add(-30 * time.Minute), Moderated: true})
	middle := mustCreateComment(t, st, &store.Comment{EntryID: other.ID, Name: "Needle Jones",
		Email: "nj@example.com", Comment: "nothing to see", Posted: now.Add(-20 * time.Minute)})
	newest := mustCreateComment(t, st, &store.Comment{EntryID: entry.ID, Name: "Grace",
		Email: "grace@example.com", Comment: "unrelated", Posted: now.Add(-10 * time.Minute), Moderated: true})
	// A subscribe-only row is a thread subscription, not a comment.
	mustCreateComment(t, st, &store.Comment{EntryID: entry.ID, Name: "Needle Watcher",
		Email: "w@example.com", Posted: now, Moderated: true, Subscribe: true, SubscribeOnly: true})

	all, total, err := st.SearchComments(ctx, "", 0, 0)
	if err != nil {
		t.Fatalf("SearchComments: %v", err)
	}
	if total != 3 || len(all) != 3 {
		t.Fatalf("unfiltered: %d rows, total %d, want 3 and 3 (the subscription is not a comment)", len(all), total)
	}
	if all[0].ID != newest.ID || all[1].ID != middle.ID || all[2].ID != oldest.ID {
		t.Errorf("order = %s,%s,%s, want newest first", all[0].Name, all[1].Name, all[2].Name)
	}
	if all[0].EntryTitle != "First entry" || all[1].EntryTitle != "Other entry" {
		t.Errorf("entry titles = %q,%q, want the joined titles", all[0].EntryTitle, all[1].EntryTitle)
	}
	if all[0].EntryID != entry.ID {
		t.Errorf("EntryID = %q, want %q", all[0].EntryID, entry.ID)
	}
	if !all[2].Moderated || all[1].Moderated {
		t.Error("the list does not carry the moderated flag as stored")
	}

	// Case-insensitive, over the text and the name alike.
	found, total, err := st.SearchComments(ctx, "needle", 0, 0)
	if err != nil {
		t.Fatalf("SearchComments(needle): %v", err)
	}
	if total != 2 || len(found) != 2 {
		t.Fatalf("search needle: %d rows, total %d, want 2", len(found), total)
	}
	if found[0].ID != middle.ID || found[1].ID != oldest.ID {
		t.Errorf("search needle returned %s,%s, want the name match then the text match", found[0].Name, found[1].Name)
	}

	// A LIKE wildcard in the search is a literal, not a wildcard.
	if _, total, err = st.SearchComments(ctx, "%", 0, 0); err != nil {
		t.Fatalf("SearchComments(%%): %v", err)
	} else if total != 0 {
		t.Errorf("a %% in the search matched %d rows, want 0", total)
	}

	page, total, err := st.SearchComments(ctx, "", 1, 1)
	if err != nil {
		t.Fatalf("SearchComments(page 2): %v", err)
	}
	if total != 3 {
		t.Errorf("paged total = %d, want 3 (the total ignores the page)", total)
	}
	if len(page) != 1 || page[0].ID != middle.ID {
		t.Errorf("page 2 of 1 = %+v, want just the middle comment", page)
	}
}

// TestUnmoderatedQueueAndCount covers the moderation queue and the number
// the menu carries (PLAN §9 A14, getUnmoderatedComments and
// getNumberUnmoderated).
func TestUnmoderatedQueueAndCount(t *testing.T) {
	st := testdb.New(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	entry := mustCreateEntry(t, st, &store.Entry{Title: "Queue entry", Alias: "queue", Body: "b",
		Posted: now.Add(-time.Hour), Username: "admin", Released: true, AllowComments: true})

	mustCreateComment(t, st, &store.Comment{EntryID: entry.ID, Name: "Approved",
		Email: "a@example.com", Comment: "fine", Posted: now.Add(-30 * time.Minute), Moderated: true})
	early := mustCreateComment(t, st, &store.Comment{EntryID: entry.ID, Name: "Waiting early",
		Email: "b@example.com", Comment: "hold me", Posted: now.Add(-20 * time.Minute)})
	late := mustCreateComment(t, st, &store.Comment{EntryID: entry.ID, Name: "Waiting late",
		Email: "c@example.com", Comment: "hold me too", Posted: now.Add(-5 * time.Minute)})
	// An unmoderated subscription row would be a phantom in the queue.
	mustCreateComment(t, st, &store.Comment{EntryID: entry.ID, Name: "Watcher",
		Email: "w@example.com", Posted: now, Subscribe: true, SubscribeOnly: true})

	queue, err := st.ListUnmoderated(ctx)
	if err != nil {
		t.Fatalf("ListUnmoderated: %v", err)
	}
	if len(queue) != 2 || queue[0].ID != late.ID || queue[1].ID != early.ID {
		t.Fatalf("queue = %+v, want the two waiting comments newest first", queue)
	}
	if queue[0].EntryTitle != "Queue entry" {
		t.Errorf("queue entry title = %q, want %q", queue[0].EntryTitle, "Queue entry")
	}

	n, err := st.CountUnmoderated(ctx)
	if err != nil {
		t.Fatalf("CountUnmoderated: %v", err)
	}
	if n != 2 {
		t.Errorf("CountUnmoderated = %d, want 2", n)
	}

	if err := st.ApproveComment(ctx, early.ID); err != nil {
		t.Fatalf("ApproveComment: %v", err)
	}
	if n, err = st.CountUnmoderated(ctx); err != nil || n != 1 {
		t.Errorf("after approving one: count = %d (err %v), want 1", n, err)
	}
}
