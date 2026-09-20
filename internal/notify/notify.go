// Package notify sends the mail a new comment causes: one message per
// thread subscriber and one to the blog's owner, each personalised
// (PLAN §11 "Notification recipients", §9 C10, C12). It is blog.cfc's
// notifyEntry, with the message building left to internal/mail and the
// recipient set left to the store.
//
// It knows nothing about HTTP and imports no web package, so the public
// site, the admin and the release sweep can all reach it.
package notify

import (
	"context"
	"errors"
	"net/url"
	"sort"
	"strings"

	"github.com/4scottt/go-blogcfc/internal/config"
	"github.com/4scottt/go-blogcfc/internal/i18n"
	"github.com/4scottt/go-blogcfc/internal/mail"
	"github.com/4scottt/go-blogcfc/internal/store"
)

// Notifier sends comment notifications.
type Notifier struct {
	// Link builds an entry's permalink. The web package sets it to its
	// own EntryURL; when it is nil the id form BlogCFC's makeLink falls
	// back to is used, which every route still resolves (PLAN §8).
	// A field rather than an import: internal/web imports this package,
	// so this package cannot import internal/web.
	Link func(store.Entry) string

	cfg      *config.Config
	store    *store.Store
	settings *config.Settings
	sender   mail.Sender
}

// New builds a Notifier. A nil sender is not an error here: Comment
// reports it when it is asked to send.
func New(cfg *config.Config, st *store.Store, settings *config.Settings, sender mail.Sender) *Notifier {
	return &Notifier{cfg: cfg, store: st, settings: settings, sender: sender}
}

// ErrNoSender is what Comment returns when there is nobody to send with.
var ErrNoSender = errors.New("notify: no mail sender configured")

// Comment is notifyEntry: everyone subscribed to the thread, plus the
// blog's owner, minus the comment's own author, each getting their own
// copy of the same message (PLAN §11 "Notification recipients").
//
// adminOnly is the as-is's `adminonly`: a comment held for moderation is
// nobody's business but the owner's until it is approved, so the thread
// is left out. The owner's copy carries a Delete link and, while
// moderation is on, an Approve link; a subscriber's carries their own
// unsubscribe link (C10, C11).
//
// It returns how many messages went out. A recipient whose message fails
// does not stop the rest: the errors are joined and returned once.
func (n *Notifier) Comment(ctx context.Context, entry *store.Entry, c *store.Comment, adminOnly bool) (int, error) {
	return n.send(ctx, entry, c, adminOnly, false)
}

// CommentApproved is notifyEntry's other call, `noadmin`: the owner has
// just approved a held comment in the moderation queue, so the thread
// hears about it and the owner -- who is doing the approving -- does not
// (PLAN §9 C12, admin/moderate.cfm). The admin package calls this.
func (n *Notifier) CommentApproved(ctx context.Context, entry *store.Entry, c *store.Comment) (int, error) {
	return n.send(ctx, entry, c, false, true)
}

// send is the body of both, with notifyEntry's two flags.
func (n *Notifier) send(ctx context.Context, entry *store.Entry, c *store.Comment, adminOnly, noAdmin bool) (int, error) {
	if entry == nil || c == nil {
		return 0, errors.New("notify: no entry or comment")
	}
	if n.sender == nil {
		return 0, ErrNoSender
	}

	owner := strings.TrimSpace(n.settings.OwnerEmail())

	// email -> the comment id that recipient's unsubscribe link names.
	// The owner's is empty: the as-is left it blank because the owner
	// has nothing to unsubscribe from.
	recipients := map[string]string{}
	if !adminOnly {
		subs, err := n.store.ThreadSubscribers(ctx, entry.ID)
		if err != nil {
			return 0, err
		}
		for email, commentID := range subs {
			recipients[email] = commentID
		}
	}
	if !noAdmin && owner != "" {
		recipients[owner] = ""
	}
	// Never mail the person who just typed the comment.
	delete(recipients, strings.TrimSpace(c.Email))
	if len(recipients) == 0 {
		return 0, nil
	}

	base := strings.TrimRight(n.cfg.BlogBaseURL, "/")
	loc := n.settings.Timezone()
	moderating := n.settings.Moderate()

	msg := mail.CommentNotification(mail.CommentNotificationVars{
		BlogTitle:   n.settings.BlogTitle(),
		EntryTitle:  entry.Title,
		EntryURL:    n.entryURL(*entry) + "#c" + c.ID,
		Author:      c.Name,
		AuthorEmail: c.Email,
		Website:     c.Website,
		Comment:     c.Comment,
		Posted:      i18n.FormatDate(n.settings.Locale(), c.Posted.In(loc), i18n.StylePosted),
		From:        mail.From(n.settings.CommentsFrom(), owner),
	})

	// A stable order so a test (and a log) reads the same way twice.
	emails := make([]string, 0, len(recipients))
	for email := range recipients {
		emails = append(emails, email)
	}
	sort.Strings(emails)

	var sent int
	var errs []error
	for _, email := range emails {
		one := mail.Personalize(msg, base, mail.Recipient{
			Email:      email,
			CommentID:  recipients[email],
			Owner:      email == owner,
			KillToken:  c.KillToken,
			ApproveID:  c.ID,
			Moderating: moderating,
		})
		if err := n.sender.Send(ctx, one); err != nil {
			errs = append(errs, err)
			continue
		}
		sent++
	}
	return sent, errors.Join(errs...)
}

// entryURL is the permalink the notification points at.
func (n *Notifier) entryURL(e store.Entry) string {
	if n.Link != nil {
		return n.Link(e)
	}
	return strings.TrimRight(n.cfg.BlogBaseURL, "/") + "/?mode=entry&entry=" + url.QueryEscape(e.ID)
}
