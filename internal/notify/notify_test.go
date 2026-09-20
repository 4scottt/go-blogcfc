package notify

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/4scottt/go-blogcfc/internal/config"
	"github.com/4scottt/go-blogcfc/internal/mail"
	"github.com/4scottt/go-blogcfc/internal/store"
	"github.com/4scottt/go-blogcfc/internal/testdb"
)

const testBase = "http://blog.example"

// newNotifier wires a notifier onto a clean database with a recording
// sender. The recipient sets and the links themselves are watched
// end-to-end by the web package's C10 test; what is watched here is the
// seam the admin package uses for C12 and the fallback permalink.
func newNotifier(t *testing.T) (*Notifier, *store.Store, *mail.Recorder, *config.Settings) {
	t.Helper()
	st := testdb.New(t)
	cfg := &config.Config{BlogBaseURL: testBase}
	settings := config.NewSettings(st, cfg)
	if err := settings.Reload(context.Background()); err != nil {
		t.Fatalf("settings reload: %v", err)
	}
	rec := &mail.Recorder{}
	return New(cfg, st, settings, rec), st, rec, settings
}

func TestCommentApprovedLeavesTheOwnerOut(t *testing.T) {
	ctx := context.Background()
	n, st, rec, settings := newNotifier(t)
	if err := settings.Set(ctx, map[string]string{"owneremail": "owner@example.com"}); err != nil {
		t.Fatalf("set owneremail: %v", err)
	}

	e := store.Entry{Title: "Approved", Body: "<p>x</p>", Username: "ray",
		Posted: time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC), Released: true, AllowComments: true}
	if err := st.CreateEntry(ctx, &e); err != nil {
		t.Fatalf("create entry: %v", err)
	}
	watcher := store.Comment{EntryID: e.ID, Name: "Pete", Email: "pete@example.com",
		Comment: "Watching.", Moderated: true, Subscribe: true}
	if err := st.CreateComment(ctx, &watcher); err != nil {
		t.Fatalf("create comment: %v", err)
	}
	held := store.Comment{EntryID: e.ID, Name: "Stranger", Email: "stranger@example.com",
		Comment: "Held one.", Moderated: true}
	if err := st.CreateComment(ctx, &held); err != nil {
		t.Fatalf("create comment: %v", err)
	}

	// The owner's own Comment call reaches both of them.
	sent, err := n.Comment(ctx, &e, &held, false)
	if err != nil || sent != 2 {
		t.Fatalf("Comment sent %d (%v), want the subscriber and the owner", sent, err)
	}

	// Approving in the queue is `noadmin`: the thread hears, the owner
	// who did the approving does not (PLAN §9 C12).
	rec.Reset()
	sent, err = n.CommentApproved(ctx, &e, &held)
	if err != nil {
		t.Fatalf("CommentApproved: %v", err)
	}
	if sent != 1 {
		t.Fatalf("CommentApproved sent %d, want the subscriber alone", sent)
	}
	msg := rec.Messages()[0]
	if len(msg.To) != 1 || msg.To[0] != "pete@example.com" {
		t.Fatalf("the approval notice went to %v", msg.To)
	}
	// With no Link set, the permalink falls back to makeLink's id form.
	if !strings.Contains(msg.Body, testBase+"/?mode=entry&entry="+e.ID+"#c"+held.ID) {
		t.Errorf("the fallback permalink is not in the body:\n%s", msg.Body)
	}
}

func TestCommentWithoutSenderFails(t *testing.T) {
	n, _, _, _ := newNotifier(t)
	n.sender = nil
	if _, err := n.Comment(context.Background(), &store.Entry{}, &store.Comment{}, false); err != ErrNoSender {
		t.Fatalf("err = %v, want ErrNoSender", err)
	}
}
