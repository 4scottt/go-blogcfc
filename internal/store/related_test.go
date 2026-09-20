package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/4scottt/go-blogcfc/internal/store"
	"github.com/4scottt/go-blogcfc/internal/testdb"
)

// TestRelatedEntriesAreBidirectionalAndLiveOnly: BlogCFC's related block
// unions both directions of the link table and never shows a draft or a
// scheduled entry.
func TestRelatedEntriesAreBidirectionalAndLiveOnly(t *testing.T) {
	st := testdb.New(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	cat := &store.Category{Name: "Support", Alias: "support"}
	if err := st.CreateCategory(ctx, cat); err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}

	subject := mustCreateEntry(t, st, &store.Entry{Title: "Subject", Alias: "subject", Body: "b",
		Posted: now.Add(-4 * time.Hour), Username: "admin", Released: true})
	forward := mustCreateEntry(t, st, &store.Entry{Title: "Forward", Alias: "forward", Body: "b",
		Posted: now.Add(-3 * time.Hour), Username: "admin", Released: true})
	backward := mustCreateEntry(t, st, &store.Entry{Title: "Backward", Alias: "backward", Body: "b",
		Posted: now.Add(-2 * time.Hour), Username: "admin", Released: true})
	draft := mustCreateEntry(t, st, &store.Entry{Title: "Draft", Alias: "draft-related", Body: "b",
		Posted: now.Add(-time.Hour), Username: "admin"})
	future := mustCreateEntry(t, st, &store.Entry{Title: "Future", Alias: "future-related", Body: "b",
		Posted: now.Add(24 * time.Hour), Username: "admin", Released: true})

	if err := st.SetEntryCategories(ctx, forward.ID, []string{cat.ID}); err != nil {
		t.Fatalf("SetEntryCategories: %v", err)
	}
	// The subject points at three; a fourth entry points back at it.
	if err := st.SetRelatedEntries(ctx, subject.ID, []string{forward.ID, draft.ID, future.ID, subject.ID, "", forward.ID}); err != nil {
		t.Fatalf("SetRelatedEntries: %v", err)
	}
	if err := st.SetRelatedEntries(ctx, backward.ID, []string{subject.ID}); err != nil {
		t.Fatalf("SetRelatedEntries: %v", err)
	}

	related, err := st.RelatedEntries(ctx, subject.ID)
	if err != nil {
		t.Fatalf("RelatedEntries: %v", err)
	}
	if len(related) != 2 {
		t.Fatalf("related = %d entries, want the forward and the backward link only", len(related))
	}
	if related[0].ID != backward.ID || related[1].ID != forward.ID {
		t.Fatalf("related = %q then %q, want newest first", related[0].Title, related[1].Title)
	}
	if len(related[1].Categories) != 1 || related[1].Categories[0].Name != "Support" {
		t.Errorf("categories are not attached to a related entry: %+v", related[1].Categories)
	}

	// Saving again replaces the set rather than adding to it.
	if err := st.SetRelatedEntries(ctx, subject.ID, nil); err != nil {
		t.Fatalf("SetRelatedEntries(nil): %v", err)
	}
	related, err = st.RelatedEntries(ctx, subject.ID)
	if err != nil {
		t.Fatalf("RelatedEntries: %v", err)
	}
	if len(related) != 1 || related[0].ID != backward.ID {
		t.Fatalf("after clearing, related = %+v, want only the backward link", related)
	}

	if got, err := st.RelatedEntries(ctx, ""); err != nil || got != nil {
		t.Fatalf("RelatedEntries(\"\") = %v (%v), want nothing", got, err)
	}

	// Deleting an entry takes its links with it, in both directions.
	if err := st.DeleteEntries(ctx, []string{backward.ID}); err != nil {
		t.Fatalf("DeleteEntries: %v", err)
	}
	if related, err = st.RelatedEntries(ctx, subject.ID); err != nil || len(related) != 0 {
		t.Fatalf("after the delete, related = %+v (%v)", related, err)
	}
}
