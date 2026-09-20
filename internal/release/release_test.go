package release_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/4scottt/go-blogcfc/internal/config"
	"github.com/4scottt/go-blogcfc/internal/mail"
	"github.com/4scottt/go-blogcfc/internal/release"
	"github.com/4scottt/go-blogcfc/internal/store"
	"github.com/4scottt/go-blogcfc/internal/testdb"
)

// testBase is the blog's base URL: every link in a mail comes from it and
// never from a request (PLAN §6).
const testBase = "http://blog.example"

// harness is a releaser on a clean database with a recording sender.
type harness struct {
	t        *testing.T
	store    *store.Store
	settings *config.Settings
	mails    *mail.Recorder
	rel      *release.Releaser
	now      time.Time
}

// newHarness wires one up. The clock starts at a fixed instant and the
// tests move it, so a scheduled entry's release does not need a sleep.
func newHarness(t *testing.T) *harness {
	t.Helper()
	st := testdb.New(t)
	cfg := &config.Config{BlogBaseURL: testBase, Port: 8080}
	settings := config.NewSettings(st, cfg)
	if err := settings.Reload(context.Background()); err != nil {
		t.Fatalf("settings reload: %v", err)
	}
	h := &harness{t: t, store: st, settings: settings, mails: &mail.Recorder{},
		now: time.Date(2011, 3, 4, 12, 0, 0, 0, time.UTC)}
	h.rel = release.New(cfg, st, settings, h.mails, nil)
	h.rel.Now = func() time.Time { return h.now }
	h.set("owneremail", "owner@example.com")
	h.set("blogtitle", "Test Blog")
	return h
}

// set writes one setting and reloads the cache.
func (h *harness) set(key, value string) {
	h.t.Helper()
	if err := h.settings.Set(context.Background(), map[string]string{key: value}); err != nil {
		h.t.Fatalf("set %s: %v", key, err)
	}
}

// entry inserts one entry.
func (h *harness) entry(e store.Entry) store.Entry {
	h.t.Helper()
	if e.Body == "" {
		e.Body = "<p>" + e.Title + " body.</p>"
	}
	if e.Username == "" {
		e.Username = "ray"
	}
	if err := h.store.CreateEntry(context.Background(), &e); err != nil {
		h.t.Fatalf("create entry %q: %v", e.Title, err)
	}
	return e
}

// subscriber signs an address up and confirms it unless told not to.
func (h *harness) subscriber(email string, verified bool) store.Subscriber {
	h.t.Helper()
	sub, _, err := h.store.AddSubscriber(context.Background(), email)
	if err != nil {
		h.t.Fatalf("add subscriber %q: %v", email, err)
	}
	if verified {
		if _, err := h.store.ConfirmSubscriber(context.Background(), sub.Token); err != nil {
			h.t.Fatalf("confirm %q: %v", email, err)
		}
	}
	return sub
}

// reload reads an entry back.
func (h *harness) reload(id string) *store.Entry {
	h.t.Helper()
	e, err := h.store.GetEntry(context.Background(), id)
	if err != nil {
		h.t.Fatalf("get entry %s: %v", id, err)
	}
	return e
}

// mailedCount is the recipient count recorded on an entry.
func (h *harness) mailedCount(id string) int {
	h.t.Helper()
	n, err := h.store.MailedCount(context.Background(), id)
	if err != nil {
		h.t.Fatalf("mailed count %s: %v", id, err)
	}
	return n
}

// TestFP_C15_ReleasedEntryMailsVerifiedSubscribersOnceAndSkipsWhenSendEmailOff
// is blog.cfc's mailEntry: a released entry that is not future-dated and
// wants mail goes to every *verified* subscriber, one message each with
// that subscriber's own unsubscribe link, and the entry is marked mailed
// with the number of addresses it reached. Marked means done: a second
// save mails nobody. `sendemail` off mails nobody and leaves the entry
// unmarked, because the as-is never reached the mark either (PLAN §9 C15).
func TestFP_C15_ReleasedEntryMailsVerifiedSubscribersOnceAndSkipsWhenSendEmailOff(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	one := h.subscriber("one@example.com", true)
	two := h.subscriber("two@example.com", true)
	h.subscriber("unconfirmed@example.com", false)

	e := h.entry(store.Entry{Title: "Released", Alias: "released", Posted: h.now.Add(-time.Hour),
		Body: "<p>First half.</p>", MoreBody: "<p>The rest.</p>", Released: true, SendEmail: true})

	if err := h.rel.OnEntrySaved(ctx, &e, false); err != nil {
		t.Fatalf("OnEntrySaved: %v", err)
	}

	msgs := h.mails.Messages()
	if len(msgs) != 2 {
		t.Fatalf("a released entry sent %d messages, want one per verified subscriber", len(msgs))
	}
	got := map[string]mail.Message{}
	for _, m := range msgs {
		if len(m.To) != 1 {
			t.Fatalf("a message went to %v, want exactly one address", m.To)
		}
		got[m.To[0]] = m
	}
	for _, sub := range []store.Subscriber{one, two} {
		m, ok := got[sub.Email]
		if !ok {
			t.Fatalf("no message for %s (got %v)", sub.Email, msgs)
		}
		if want := "Test Blog / Released"; m.Subject != want {
			t.Errorf("subject %q, want %q", m.Subject, want)
		}
		if m.From != "owner@example.com" {
			t.Errorf("message came from %q, want the owner", m.From)
		}
		link := testBase + "/unsubscribe?email=" + strings.ReplaceAll(sub.Email, "@", "%40") +
			"&token=" + sub.Token
		if !strings.Contains(m.Body, link) {
			t.Errorf("%s's message has no unsubscribe link %q:\n%s", sub.Email, link, m.Body)
		}
		for _, want := range []string{testBase + "/2011/3/4/released", "First half.", "[Continued at Blog]"} {
			if !strings.Contains(m.Body, want) {
				t.Errorf("%s's message is missing %q:\n%s", sub.Email, want, m.Body)
			}
		}
	}
	// The links are per recipient, not one link for everybody.
	if got[one.Email].Body == got[two.Email].Body {
		t.Error("both subscribers got the same body, so the unsubscribe links cannot be theirs")
	}
	if strings.Contains(got[one.Email].Body, two.Token) {
		t.Error("one subscriber's message carries another's token")
	}
	if strings.Contains(got[one.Email].Body, "unconfirmed@example.com") {
		t.Error("an unconfirmed address was mailed")
	}

	saved := h.reload(e.ID)
	if !saved.Mailed {
		t.Error("the entry was not marked mailed")
	}
	if n := h.mailedCount(e.ID); n != 2 {
		t.Errorf("mailed_count = %d, want 2", n)
	}

	// Once, and once only: a second save of the same entry mails nobody.
	h.mails.Reset()
	if err := h.rel.OnEntrySaved(ctx, saved, true); err != nil {
		t.Fatalf("OnEntrySaved (again): %v", err)
	}
	if n := len(h.mails.Messages()); n != 0 {
		t.Errorf("saving a mailed entry again sent %d messages, want 0", n)
	}
	// Nor does the sweep pick it up.
	if n, err := h.rel.Sweep(ctx); err != nil || n != 0 {
		t.Errorf("Sweep after mailing = %d (%v), want 0", n, err)
	}

	// `sendemail` off: nothing is sent, and the entry is not marked --
	// the as-is never called mailEntry, so it never reached the mark.
	h.mails.Reset()
	quiet := h.entry(store.Entry{Title: "Quiet", Alias: "quiet", Posted: h.now.Add(-time.Hour),
		Released: true, SendEmail: false})
	if err := h.rel.OnEntrySaved(ctx, &quiet, false); err != nil {
		t.Fatalf("OnEntrySaved (sendemail off): %v", err)
	}
	if n := len(h.mails.Messages()); n != 0 {
		t.Errorf("an entry with sendemail off sent %d messages, want 0", n)
	}
	if h.reload(quiet.ID).Mailed {
		t.Error("an entry with sendemail off was marked mailed")
	}

	// A blog with no subscribers at all: still marked, and the count says
	// nobody heard about it (PLAN §7, the mailed=1 bug kept but recorded).
	for _, sub := range []store.Subscriber{one, two} {
		if err := h.store.DeleteSubscriber(ctx, sub.Email); err != nil {
			t.Fatalf("delete subscriber: %v", err)
		}
	}
	h.mails.Reset()
	lonely := h.entry(store.Entry{Title: "Lonely", Alias: "lonely", Posted: h.now.Add(-time.Hour),
		Released: true, SendEmail: true})
	count, err := h.rel.MailNow(ctx, &lonely)
	if err != nil {
		t.Fatalf("MailNow: %v", err)
	}
	if count != 0 || len(h.mails.Messages()) != 0 {
		t.Errorf("MailNow with no subscribers sent %d messages (count %d), want none", len(h.mails.Messages()), count)
	}
	if !h.reload(lonely.ID).Mailed {
		t.Error("an entry mailed to nobody was left unmarked")
	}
	if n := h.mailedCount(lonely.ID); n != 0 {
		t.Errorf("mailed_count = %d, want 0", n)
	}
}

// TestFP_C16_SweepMailsScheduledEntryWhenDue is the replacement for
// BlogCFC's per-entry cfschedule task and admin/notify.cfm: saving a
// released entry dated in the future mails nobody, and the one-minute
// sweep mails it exactly once when its time comes (PLAN §7, §9 C16).
func TestFP_C16_SweepMailsScheduledEntryWhenDue(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.subscriber("reader@example.com", true)

	e := h.entry(store.Entry{Title: "Scheduled", Alias: "scheduled",
		Posted: h.now.Add(time.Minute), Released: true, SendEmail: true})

	if err := h.rel.OnEntrySaved(ctx, &e, false); err != nil {
		t.Fatalf("OnEntrySaved: %v", err)
	}
	if n := len(h.mails.Messages()); n != 0 {
		t.Fatalf("saving a future entry sent %d messages, want 0", n)
	}
	if h.reload(e.ID).Mailed {
		t.Fatal("a future entry was marked mailed on save")
	}

	// A sweep before its time does nothing.
	if n, err := h.rel.Sweep(ctx); err != nil || n != 0 {
		t.Fatalf("Sweep before the entry is due = %d (%v), want 0", n, err)
	}
	if n := len(h.mails.Messages()); n != 0 {
		t.Fatalf("a sweep before the entry was due sent %d messages, want 0", n)
	}

	// Two minutes on, the sweep mails it.
	h.now = h.now.Add(2 * time.Minute)
	n, err := h.rel.Sweep(ctx)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if n != 1 {
		t.Errorf("Sweep mailed %d entries, want 1", n)
	}
	msgs := h.mails.Messages()
	if len(msgs) != 1 || len(msgs[0].To) != 1 || msgs[0].To[0] != "reader@example.com" {
		t.Fatalf("the sweep sent %v, want one message to the subscriber", msgs)
	}
	if !strings.Contains(msgs[0].Body, testBase+"/2011/3/4/scheduled") {
		t.Errorf("the mail has no permalink:\n%s", msgs[0].Body)
	}
	saved := h.reload(e.ID)
	if !saved.Mailed {
		t.Error("the swept entry was not marked mailed")
	}
	if got := h.mailedCount(e.ID); got != 1 {
		t.Errorf("mailed_count = %d, want 1", got)
	}

	// And only once: a third sweep sends nothing.
	h.mails.Reset()
	h.now = h.now.Add(time.Minute)
	if n, err := h.rel.Sweep(ctx); err != nil || n != 0 {
		t.Errorf("the third sweep mailed %d entries (%v), want 0", n, err)
	}
	if n := len(h.mails.Messages()); n != 0 {
		t.Errorf("the third sweep sent %d messages, want 0", n)
	}
}

// TestFP_C17_PingsEachURLOnRelease is ping.cfc, minus its three dead
// aggregators: every URL in `pingurls` is GET-requested when an entry is
// released, and one that fails does not stop the rest (PLAN §7, §9 C17).
func TestFP_C17_PingsEachURLOnRelease(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	var first, second, refused atomic.Int32
	one := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("ping used %s, want GET", r.Method)
		}
		first.Add(1)
	}))
	defer one.Close()
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refused.Add(1)
		http.Error(w, "no", http.StatusInternalServerError)
	}))
	defer broken.Close()
	two := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		second.Add(1)
	}))
	defer two.Close()
	// A server that is not listening at all: the ping cannot even connect.
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close()

	h.set("pingurls", strings.Join([]string{
		one.URL + "/ping", deadURL + "/ping", broken.URL + "/ping", "@technorati", "", two.URL + "/ping",
	}, "\n"))

	e := h.entry(store.Entry{Title: "Pinged", Alias: "pinged", Posted: h.now.Add(-time.Minute),
		Released: true, SendEmail: false})
	if err := h.rel.OnEntrySaved(ctx, &e, false); err != nil {
		t.Fatalf("OnEntrySaved: %v", err)
	}

	if first.Load() != 1 {
		t.Errorf("the first ping target was hit %d times, want 1", first.Load())
	}
	if second.Load() != 1 {
		t.Errorf("the last ping target was hit %d times, want 1: a failing URL stopped the others", second.Load())
	}
	if refused.Load() != 1 {
		t.Errorf("the failing target was hit %d times, want 1", refused.Load())
	}

	// A re-save of an entry that was already out does not ping again: the
	// as-is re-announced an entry every time it was edited.
	if err := h.rel.OnEntrySaved(ctx, &e, true); err != nil {
		t.Fatalf("OnEntrySaved (again): %v", err)
	}
	if first.Load() != 1 || second.Load() != 1 {
		t.Errorf("re-saving a released entry pinged again (%d, %d)", first.Load(), second.Load())
	}

	// A future entry pings when the sweep releases it, not before.
	h.set("pingurls", one.URL+"/scheduled")
	later := h.entry(store.Entry{Title: "Later", Alias: "later", Posted: h.now.Add(time.Minute),
		Released: true, SendEmail: true})
	if err := h.rel.OnEntrySaved(ctx, &later, false); err != nil {
		t.Fatalf("OnEntrySaved (future): %v", err)
	}
	if first.Load() != 1 {
		t.Errorf("a future entry pinged on save (%d hits)", first.Load())
	}
	h.now = h.now.Add(2 * time.Minute)
	if _, err := h.rel.Sweep(ctx); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if first.Load() != 2 {
		t.Errorf("the sweep did not ping when the entry went out (%d hits, want 2)", first.Load())
	}
}

// TestReleaseRunSweepsUntilContextDone is the goroutine main.go starts:
// it sweeps on its interval and stops when the context does.
func TestReleaseRunSweepsUntilContextDone(t *testing.T) {
	h := newHarness(t)
	h.subscriber("reader@example.com", true)
	e := h.entry(store.Entry{Title: "Ticked", Alias: "ticked", Posted: h.now.Add(-time.Minute),
		Released: true, SendEmail: true})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		h.rel.Run(ctx, 10*time.Millisecond)
		close(done)
	}()

	deadline := time.After(5 * time.Second)
	for {
		if len(h.mails.Messages()) > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("Run never swept")
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop when its context was cancelled")
	}
	if !h.reload(e.ID).Mailed {
		t.Error("the swept entry was not marked mailed")
	}
}
