package web

import (
	"bytes"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
)

// The subscription links a mail carries: confirming a blog subscription
// and unsubscribing from a blog or from a comment thread (PLAN §9 C13,
// C14; client/confirmsubscription.cfm, client/unsubscribe.cfm).

// subscriptionPage is the one page both handlers render: a heading, a
// sentence, and the as-is "Return to the Blog" link under it.
type subscriptionPage struct {
	pageData

	Heading string
	Message string

	ReturnURL   string
	ReturnLabel string
}

// subscriptionTemplate is parsed once at start, beside the ones web.go
// and routes_extra.go own. A template that does not parse is a
// build-time mistake, as it is there.
var subscriptionTemplate = template.Must(template.New("layout.html").
	ParseFS(templateFS, "templates/layout.html", "templates/subscription.html"))

// renderSubscription writes the page through a buffer, so a template
// error cannot leave half a page on the wire.
func (m *Module) renderSubscription(w http.ResponseWriter, page subscriptionPage) {
	var buf bytes.Buffer
	if err := subscriptionTemplate.ExecuteTemplate(&buf, "layout.html", page); err != nil {
		slog.Error("web: render failed", "page", "subscription.html", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := buf.WriteTo(w); err != nil {
		slog.Debug("web: write failed", "error", err)
	}
}

// newSubscriptionPage fills the parts both answers share.
func (m *Module) newSubscriptionPage(r *http.Request, heading, message string) subscriptionPage {
	return subscriptionPage{
		pageData:    m.newPage(r, heading),
		Heading:     heading,
		Message:     message,
		ReturnURL:   m.base() + "/",
		ReturnLabel: m.bundle.T("returntoblog"),
	}
}

// handleConfirmSubscription is `GET /confirmsubscription?t={token}`:
// BlogCFC's confirmsubscription.cfm, the second half of the double
// opt-in the subscribe pod starts (PLAN §9 C13).
//
// Without a token it redirects home, as the as-is `cflocation` did. With
// one it verifies the address the token names and says so. The as-is
// printed its thank-you whatever the token was -- the update simply
// matched no row -- which told a spammer nothing but told an honest
// subscriber with a mangled link nothing either; an unknown token gets
// its own sentence here.
func (m *Module) handleConfirmSubscription(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("t"))
	if token == "" {
		http.Redirect(w, r, m.base()+"/", http.StatusFound)
		return
	}

	// The bundle line ends in the trailing spaces the as-is properties
	// file has; the heading drops them, as the subscribe pod does.
	heading := strings.TrimSpace(m.bundle.T("subscribeconfirm"))
	confirmed, err := m.store.ConfirmSubscriber(r.Context(), token)
	if err != nil {
		m.serverError(w, r, err)
		return
	}
	message := m.bundle.T("subscribeconfirmbody")
	if !confirmed {
		message = m.textOr("subscribeconfirmfailure",
			"Your subscription was not confirmed. Please ensure you correctly copied the URL from the email.")
	}
	m.renderSubscription(w, m.newSubscriptionPage(r, heading, message))
}

// handleUnsubscribe is `GET /unsubscribe?email=&commentID=` and
// `?email=&token=`: BlogCFC's unsubscribe.cfm, which answered both the
// per-comment link in a notification and the per-subscriber link in an
// entry mail (PLAN §9 C14).
//
//   - `commentID`: the comment id and the address must agree, and then
//     that address stops being subscribed to the entry's thread.
//   - `token`: the address and its token must agree, and then the
//     subscriber row goes.
//
// Either way a pair that does not match changes nothing and gets the
// as-is "please check the URL" sentence. No address at all redirects
// home, as the as-is did.
func (m *Module) handleUnsubscribe(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	email := strings.TrimSpace(q.Get("email"))
	if email == "" {
		http.Redirect(w, r, m.base()+"/", http.StatusFound)
		return
	}
	commentID := strings.TrimSpace(q.Get("commentID"))
	if commentID == "" {
		commentID = strings.TrimSpace(q.Get("commentid"))
	}
	token := strings.TrimSpace(q.Get("token"))

	heading := m.bundle.T("unsubscribe")
	var message string
	switch {
	case commentID != "":
		ok, err := m.store.UnsubscribeThread(r.Context(), commentID, email)
		if err != nil {
			m.serverError(w, r, err)
			return
		}
		message = m.bundle.T("unsubscribefailure")
		if ok {
			message = m.bundle.T("unsubscribesuccess")
		}
	case token != "":
		ok, err := m.store.RemoveSubscriberByToken(r.Context(), email, token)
		if err != nil {
			m.serverError(w, r, err)
			return
		}
		message = m.bundle.T("unsubscribeblogfailure")
		if ok {
			message = m.bundle.T("unsubscribeblogsuccess")
		}
	default:
		// An address with neither a comment nor a token: the as-is
		// printed its heading and the return link and nothing else.
		// Saying which link is broken is kinder and gives nothing away.
		message = m.bundle.T("unsubscribeblogfailure")
	}
	m.renderSubscription(w, m.newSubscriptionPage(r, heading, message))
}

// textOr is the bundle with a fallback, for a string BlogCFC never had:
// the localized value when the bundle carries the key, the English one
// otherwise (the pods package does the same).
func (m *Module) textOr(key, asIs string) string {
	if m.bundle.Has(key) {
		return m.bundle.T(key)
	}
	return asIs
}
