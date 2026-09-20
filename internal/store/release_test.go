package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/4scottt/go-blogcfc/internal/store"
	"github.com/4scottt/go-blogcfc/internal/testdb"
)

// entryFor is one entry in whatever release state the case wants.
func entryFor(t *testing.T, st *store.Store, title string, posted time.Time, released, mailed, sendEmail bool) store.Entry {
	t.Helper()
	e := store.Entry{Title: title, Alias: title, Body: "<p>" + title + "</p>", Posted: posted,
		Username: "ray", Released: released, Mailed: mailed, SendEmail: sendEmail}
	if err := st.CreateEntry(context.Background(), &e); err != nil {
		t.Fatalf("create entry %q: %v", title, err)
	}
	return e
}

// TestReleaseUnmailedReleasedAndMarkMailed is the sweep's query and the
// mark it leaves: released, due, unmailed and `sendemail` on, and the
// recipient count recorded beside the flag (PLAN §9 C15, C16).
func TestReleaseUnmailedReleasedAndMarkMailed(t *testing.T) {
	st := testdb.New(t)
	ctx := context.Background()
	now := time.Date(2011, 3, 4, 12, 0, 0, 0, time.UTC)

	due := entryFor(t, st, "due", now.Add(-time.Hour), true, false, true)
	entryFor(t, st, "draft", now.Add(-time.Hour), false, false, true)
	entryFor(t, st, "scheduled", now.Add(time.Hour), true, false, true)
	entryFor(t, st, "already-mailed", now.Add(-time.Hour), true, true, true)
	entryFor(t, st, "no-mail-wanted", now.Add(-time.Hour), true, false, false)
	older := entryFor(t, st, "older", now.Add(-2*time.Hour), true, false, true)

	got, err := st.UnmailedReleased(ctx, now)
	if err != nil {
		t.Fatalf("UnmailedReleased: %v", err)
	}
	want := []string{"older", "due"} // oldest first
	if names := titles(got); len(names) != len(want) || names[0] != want[0] || names[1] != want[1] {
		t.Fatalf("UnmailedReleased = %v, want %v", names, want)
	}
	if got[0].Body == "" || got[0].Posted.Location() != time.UTC {
		t.Errorf("a swept entry came back thin: %+v", got[0])
	}

	// The scheduled entry becomes due when its time comes.
	later, err := st.UnmailedReleased(ctx, now.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("UnmailedReleased (later): %v", err)
	}
	if len(later) != 3 {
		t.Errorf("two hours later the sweep sees %v, want the scheduled entry as well", titles(later))
	}

	// Marking takes an entry out of the sweep and records the count.
	if err := st.MarkMailed(ctx, due.ID, 3); err != nil {
		t.Fatalf("MarkMailed: %v", err)
	}
	e, err := st.GetEntry(ctx, due.ID)
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	if !e.Mailed {
		t.Error("MarkMailed did not set the flag")
	}
	if n, err := st.MailedCount(ctx, due.ID); err != nil || n != 3 {
		t.Errorf("MailedCount = %d (%v), want 3", n, err)
	}
	if names := titles(mustSweep(t, st, now)); len(names) != 1 || names[0] != "older" {
		t.Errorf("after marking, the sweep sees %v, want [older]", names)
	}

	// Zero subscribers still marks, and the count says so: BlogCFC set
	// `mailed` with nobody on the list and left no trace of it.
	if err := st.MarkMailed(ctx, older.ID, 0); err != nil {
		t.Fatalf("MarkMailed(0): %v", err)
	}
	if n, err := st.MailedCount(ctx, older.ID); err != nil || n != 0 {
		t.Errorf("MailedCount = %d (%v), want 0", n, err)
	}
	if got := mustSweep(t, st, now); len(got) != 0 {
		t.Errorf("after marking both, the sweep sees %v, want nothing", titles(got))
	}

	if err := st.MarkMailed(ctx, "00000000-0000-4000-8000-000000000000", 1); err == nil {
		t.Error("MarkMailed on an entry that does not exist did not say so")
	}
}

func mustSweep(t *testing.T, st *store.Store, now time.Time) []store.Entry {
	t.Helper()
	got, err := st.UnmailedReleased(context.Background(), now)
	if err != nil {
		t.Fatalf("UnmailedReleased: %v", err)
	}
	return got
}

// TestReleaseUnsubscribeThread is blog.cfc's unsubscribeThread: the
// comment id and the address must agree, and then every comment that
// address left on the entry stops being subscribed (PLAN §9 C14).
func TestReleaseUnsubscribeThread(t *testing.T) {
	st := testdb.New(t)
	ctx := context.Background()
	e := entryFor(t, st, "thread", time.Date(2011, 3, 4, 9, 0, 0, 0, time.UTC), true, false, false)

	mk := func(email string, posted time.Time) store.Comment {
		c := store.Comment{EntryID: e.ID, Name: "Reader", Email: email, Comment: "Hi",
			Posted: posted, Subscribe: true, Moderated: true}
		if err := st.CreateComment(ctx, &c); err != nil {
			t.Fatalf("create comment: %v", err)
		}
		return c
	}
	first := mk("reader@example.com", time.Date(2011, 3, 4, 10, 0, 0, 0, time.UTC))
	second := mk("reader@example.com", time.Date(2011, 3, 4, 11, 0, 0, 0, time.UTC))
	other := mk("other@example.com", time.Date(2011, 3, 4, 12, 0, 0, 0, time.UTC))

	subscribed := func(id string) bool {
		c, err := st.GetComment(ctx, id)
		if err != nil {
			t.Fatalf("GetComment: %v", err)
		}
		return c.Subscribe
	}

	for _, bad := range [][2]string{
		{first.ID, "nobody@example.com"},
		{"00000000-0000-4000-8000-000000000000", "reader@example.com"},
		{"", "reader@example.com"},
		{first.ID, ""},
	} {
		ok, err := st.UnsubscribeThread(ctx, bad[0], bad[1])
		if err != nil {
			t.Fatalf("UnsubscribeThread(%q, %q): %v", bad[0], bad[1], err)
		}
		if ok {
			t.Errorf("UnsubscribeThread(%q, %q) = true, want false", bad[0], bad[1])
		}
	}
	if !subscribed(first.ID) || !subscribed(second.ID) || !subscribed(other.ID) {
		t.Fatal("a pair that does not match changed a subscription")
	}

	// The as-is matched the address in SQL, under a case-insensitive
	// collation; so does this.
	ok, err := st.UnsubscribeThread(ctx, second.ID, "Reader@Example.com")
	if err != nil || !ok {
		t.Fatalf("UnsubscribeThread = %v (%v), want true", ok, err)
	}
	if subscribed(first.ID) || subscribed(second.ID) {
		t.Error("the address is still subscribed to the thread")
	}
	if !subscribed(other.ID) {
		t.Error("another address lost its subscription")
	}
}
