package pods

import (
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/4scottt/go-blogcfc/internal/mail"
)

// The subscribe pod and the form it posts to (PLAN §9 D07,
// client/includes/pods/subscribe.cfm).

// subscribePath is where the pod's form posts. BlogCFC posted back to
// the page the pod sat on and showed its message inside the pod; a route
// of its own keeps the pod stateless and the handler testable.
const subscribePath = "/subscribe"

// Routes registers the subscribe form's target.
func (m *Module) Routes(mux *http.ServeMux) {
	mux.HandleFunc("POST "+subscribePath, m.handleSubscribe)
}

// subscribePod draws the form.
func (m *Module) subscribePod(c *podCtx) (string, template.HTML) {
	label := c.bundle.T("subscribe")
	return label, m.exec("subscribe", struct {
		ActionURL string
		Intro     string
		Label     string
	}{c.base + subscribePath, c.bundle.T("subscribeblog"), label})
}

// handleSubscribe signs an address up and answers with a small page: the
// confirmation is on its way, the address was already on the list, or the
// address was not an address (PLAN §9 D07, C13).
func (m *Module) handleSubscribe(w http.ResponseWriter, r *http.Request) {
	c := m.newCtx(r)
	email := strings.TrimSpace(r.FormValue("email"))

	if !validEmail(email) {
		m.result(w, c, c.bundle.T("mustincludeemail"), true)
		return
	}
	sub, existed, err := m.store.AddSubscriber(c.ctx, email)
	if err != nil {
		slog.Warn("pods: subscribe failed", "error", err)
		m.result(w, c, c.bundle.T("errorpagebody"), true)
		return
	}
	if existed {
		// addSubscriber returned no token for an address it already had,
		// and the as-is pod then said nothing at all; saying so is kinder
		// and does not leak more than the form already does.
		m.result(w, c, text(c.bundle, "alreadysubscribed", "You are already subscribed to this blog."), false)
		return
	}
	m.sendConfirmation(c, sub.Email, sub.Token)
	m.result(w, c, c.bundle.T("subscribeconfirmation"), false)
}

// sendConfirmation mails the double opt-in link (PLAN §9 C13). A sender
// that fails costs the mail, not the page: the address is on the list and
// subscribing again sends a fresh link.
func (m *Module) sendConfirmation(c *podCtx, email, token string) {
	if m.sender == nil {
		slog.Warn("pods: no mail sender configured, subscription not confirmed", "email", email)
		return
	}
	msg := mail.Message{
		To:      []string{email},
		From:    m.settings.OwnerEmail(),
		Subject: strings.TrimSpace(m.settings.BlogTitle() + " " + strings.TrimSpace(c.bundle.T("subscribeconfirm"))),
		Body: c.bundle.T("subscribeconfirmation") + "\n\n" +
			c.base + "/confirmsubscription?t=" + url.QueryEscape(token) + "\n",
	}
	if err := m.sender.Send(c.ctx, msg); err != nil {
		slog.Warn("pods: confirmation mail failed", "email", email, "error", err)
	}
}

// result writes the little page the form answers with.
func (m *Module) result(w http.ResponseWriter, c *podCtx, message string, failed bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	page := m.exec("subscribed", struct {
		BlogTitle string
		Title     string
		Message   string
		Failed    bool
		CSSURL    string
		HomeURL   string
		HomeLabel string
	}{
		BlogTitle: m.settings.BlogTitle(),
		Title:     c.bundle.T("subscribe"),
		Message:   message,
		Failed:    failed,
		CSSURL:    c.base + "/static/css/site.css",
		HomeURL:   c.base + "/",
		HomeLabel: text(c.bundle, "home", "Home"),
	})
	if _, err := w.Write([]byte(page)); err != nil {
		slog.Warn("pods: write subscribe result", "error", err)
	}
}

// emailPattern is BlogCFC's isEmail (org/camden/blog/utils.cfc), kept as
// it was: the same local-part characters, the same two-to-three letter
// or long-gTLD ending. It refuses addresses a 2011 blog refused.
var emailPattern = regexp.MustCompile(`(?i)^['_a-z0-9-]+(\.['_a-z0-9-]+)*(\+['_a-z0-9-]+)*@[a-z0-9-]+(\.[a-z0-9-]+)*\.(([a-z]{2,3})|(aero|asia|biz|cat|coop|info|museum|name|jobs|post|pro|tel|travel|mobi))$`)

// validEmail is isEmail: the pattern plus its length limits.
func validEmail(s string) bool {
	if !emailPattern.MatchString(s) {
		return false
	}
	local, domain, _ := strings.Cut(s, "@")
	return len(local) <= 64 && len(domain) <= 255
}
