// Package release is what happens to the world when an entry goes live
// (PLAN §11 "Release side effects"): the mail to the blog's verified
// subscribers, the sweep that mails a scheduled entry when its time
// comes, and the pings the `pingurls` setting names.
//
// It replaces two pieces of BlogCFC: the `cfschedule` task blog.cfc
// created per future entry, which called the unauthenticated
// admin/notify.cfm, and ping.cfc's aggregator pings. The schedule and the
// open endpoint are gone; a one-minute sweep over `released = 1,
// posted <= now, mailed = 0, sendemail = 1` does the same work from
// inside the process (PLAN §7).
package release

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/4scottt/go-blogcfc/internal/config"
	"github.com/4scottt/go-blogcfc/internal/mail"
	"github.com/4scottt/go-blogcfc/internal/render"
	"github.com/4scottt/go-blogcfc/internal/store"
	"github.com/4scottt/go-blogcfc/internal/web"
)

// pingTimeout bounds one ping. A save waits for the pings, as the as-is
// page did before it grew threads, so the budget is small.
const pingTimeout = 5 * time.Second

// Releaser carries the release side effects. Its zero value is not
// usable; build one with New.
type Releaser struct {
	cfg      *config.Config
	store    *store.Store
	settings *config.Settings
	sender   mail.Sender
	client   *http.Client

	// Now is the clock. It is nil in production, where time.Now is the
	// answer; a test sets it to walk the sweep past a scheduled entry's
	// posted date without sleeping.
	Now func() time.Time
}

// New builds a releaser. A nil client means a plain one with the ping
// timeout; a nil sender means nothing is mailed, which is what a process
// without a configured Sender should do rather than crash.
func New(cfg *config.Config, st *store.Store, settings *config.Settings, sender mail.Sender, client *http.Client) *Releaser {
	if client == nil {
		client = &http.Client{Timeout: pingTimeout}
	}
	return &Releaser{cfg: cfg, store: st, settings: settings, sender: sender, client: client}
}

// now is the clock, time.Now unless a test replaced it.
func (r *Releaser) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

// OnEntrySaved runs the release side effects for an entry the admin has
// just written, in PLAN §11's order. releasedBefore is the entry's
// `released` flag as it was before the save, which is what tells a first
// release from a re-save of an entry that was already out.
//
//   - released, posted at or before now, `sendemail` on, not mailed yet:
//     mail the subscribers now.
//   - released and posted in the future: nothing. The sweep owns it.
//   - released, posted at or before now, not released before: ping.
//
// A ping never fails the save. A mail failure does come back, so the
// caller can say so: the entry is saved either way.
func (r *Releaser) OnEntrySaved(ctx context.Context, e *store.Entry, releasedBefore bool) error {
	if e == nil || !e.Released {
		return nil
	}
	if e.Posted.After(r.now()) {
		// Scheduled: the sweep mails it when its time comes, and pings
		// then too. BlogCFC booked a cfschedule task here instead.
		return nil
	}

	var err error
	if e.SendEmail && !e.Mailed {
		if _, err = r.MailNow(ctx, e); err != nil {
			slog.Error("release: mailing subscribers failed", "entry", e.ID, "error", err)
		}
	}
	if !releasedBefore {
		// The as-is pinged on every save of a released, non-future entry,
		// which re-announced an entry every time a typo was fixed. Only a
		// first release pings here (PLAN §11).
		r.Ping(ctx, e)
	}
	return err
}

// MailNow sends one entry to every verified subscriber and marks it
// mailed, and says how many addresses it reached (blog.cfc mailEntry,
// PLAN §9 C15). An entry that is already marked is skipped: the mark is
// what makes the mail happen once, whether the sweep or a save gets there
// first.
//
// Each subscriber gets their own message, carrying their own unsubscribe
// link (their address and their token). A recipient whose send fails is
// logged and does not stop the others; the mark still goes on afterwards,
// because a blog that mails a sender that is down on every sweep is worse
// than one that misses a message.
func (r *Releaser) MailNow(ctx context.Context, e *store.Entry) (int, error) {
	if e == nil || e.Mailed {
		return 0, nil
	}
	subscribers, err := r.store.VerifiedSubscribers(ctx)
	if err != nil {
		return 0, err
	}

	sent := 0
	if len(subscribers) > 0 && r.sender == nil {
		slog.Warn("release: no mail sender configured, entry not mailed", "entry", e.ID)
	}
	if r.sender != nil {
		vars := r.entryVars(ctx, e)
		for _, sub := range subscribers {
			v := vars
			v.Email, v.Token = sub.Email, sub.Token
			if err := r.sender.Send(ctx, mail.NewEntry(v)); err != nil {
				slog.Error("release: entry mail failed", "entry", e.ID, "to", sub.Email, "error", err)
				continue
			}
			sent++
		}
	}

	// The mark goes on even when sent is 0 (the as-is did too); the count
	// beside it is the fix for BlogCFC's "mailed with no subscribers".
	if err := r.store.MarkMailed(ctx, e.ID, sent); err != nil {
		return sent, err
	}
	e.Mailed = true
	return sent, nil
}

// entryVars is one entry as its subscribers' mail shows it: the title,
// the permalink, the author and the rendered body, with [Continued at
// Blog] when there is more (mailEntry).
//
// The body goes through the same render pipeline the site uses, minus the
// enclosure decoration the as-is passed in: these messages are plain text
// (PLAN §11 "Mail"), so an audio player or an <img> in them is noise.
func (r *Releaser) entryVars(ctx context.Context, e *store.Entry) mail.NewEntryVars {
	base := strings.TrimRight(r.cfg.BlogBaseURL, "/")
	body := render.Entry(e.Body, render.Options{Textblocks: r.textblocks(ctx)})
	return mail.NewEntryVars{
		BaseURL:   base,
		BlogTitle: r.settings.BlogTitle(),
		Title:     e.Title,
		EntryURL:  web.EntryURL(base, *e, r.settings.Timezone()),
		Author:    e.Username,
		Body:      string(body),
		HasMore:   e.MoreBody != "",
		From:      r.settings.OwnerEmail(),
	}
}

// textblocks resolves `<textblock label="x">` in a mailed body, as the
// site does. An unreadable table resolves nothing rather than losing the
// mail.
func (r *Releaser) textblocks(ctx context.Context) func(string) (string, bool) {
	blocks, err := r.store.TextblockMap(ctx)
	if err != nil {
		slog.Error("release: textblock lookup failed", "error", err)
		return nil
	}
	if len(blocks) == 0 {
		return nil
	}
	return func(label string) (string, bool) {
		body, ok := blocks[label]
		return body, ok
	}
}

// Ping GETs every URL in the `pingurls` setting when an entry is
// released. BlogCFC pinged Technorati, weblogs.com and IceRocket, all
// dead; `pingurls` is now a plain newline list of URLs to call, and its
// three old `@name` sentinels are skipped rather than followed (PLAN §7,
// §9 C17).
//
// A ping that fails is logged and nothing more: an aggregator being down
// has never been a reason to lose a post.
func (r *Releaser) Ping(ctx context.Context, e *store.Entry) {
	for _, raw := range r.settings.PingURLs() {
		target := strings.TrimSpace(raw)
		if target == "" {
			continue
		}
		if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
			slog.Warn("release: skipping ping target that is not an http url", "target", target)
			continue
		}
		r.ping(ctx, target, e)
	}
}

// ping calls one URL and throws the body away.
func (r *Releaser) ping(ctx context.Context, target string, e *store.Entry) {
	rctx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, target, nil)
	if err != nil {
		slog.Warn("release: bad ping target", "target", target, "error", err)
		return
	}
	resp, err := r.client.Do(req)
	if err != nil {
		slog.Warn("release: ping failed", "target", target, "error", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		slog.Warn("release: ping refused", "target", target, "status", resp.StatusCode)
		return
	}
	slog.Debug("release: pinged", "target", target, "entry", e.ID)
}

// Sweep mails every released entry whose time has come and which has not
// been mailed: notify.cfm's checks, run from the inside every minute. It
// returns the number of entries mailed, not the number of messages.
func (r *Releaser) Sweep(ctx context.Context) (int, error) {
	now := r.now()
	due, err := r.store.UnmailedReleased(ctx, now)
	if err != nil {
		return 0, err
	}
	mailed := 0
	for i := range due {
		e := due[i]
		count, err := r.MailNow(ctx, &e)
		if err != nil {
			slog.Error("release: sweep failed to mail an entry", "entry", e.ID, "error", err)
			continue
		}
		mailed++
		slog.Info("release: mailed a scheduled entry", "entry", e.ID, "recipients", count)
		// A scheduled entry reaches the world here, so this is where it
		// pings: the save that booked it was too early to.
		r.Ping(ctx, &e)
	}
	return mailed, nil
}

// Run sweeps every `every` until ctx is done. main.go starts it in a
// goroutine; a sweep that fails is logged and the next one still runs.
func (r *Releaser) Run(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = time.Minute
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n, err := r.Sweep(ctx); err != nil {
				slog.Error("release: sweep failed", "error", err)
			} else if n > 0 {
				slog.Info("release: sweep mailed entries", "entries", n)
			}
		}
	}
}
