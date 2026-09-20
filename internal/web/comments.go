package web

import (
	"bytes"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/4scottt/go-blogcfc/internal/antispam"
	"github.com/4scottt/go-blogcfc/internal/notify"
	"github.com/4scottt/go-blogcfc/internal/store"
)

// The comment pipeline (PLAN §11 "Comment pipeline", §9 C01-C11):
// BlogCFC's addcomment.cfm and addsub.cfm, which were popup windows off
// the entry page and are pages of their own here as well, plus the two
// one-click query forms the owner's notification mail links to.
//
// Nothing a commenter types is escaped on the way in: the store keeps the
// text and html/template escapes it on the way out, where index.cfm
// stored `htmlEditFormat`ed text and printed it raw (PLAN §7 "Bugs
// fixed", §9 C02).

// The remember-me cookies, with BlogCFC's own names so a reader who has
// commented on the as-is is still remembered (PLAN §9 C07).
const (
	cookieName    = "blog_name"
	cookieEmail   = "blog_email"
	cookieWebsite = "blog_website"
)

// rememberLifetime stands in for BlogCFC's `expires="never"`, which was a
// cookie 30 years out. Ten years is the longest a browser keeps one now.
const rememberLifetime = 10 * 365 * 24 * time.Hour

// websitePlaceholder is what addcomment.cfm put in the website box and
// stripped again on submit, so an untouched box does not become a link.
const websitePlaceholder = "http://"

// The as-is's field lengths (blog.cfc addComment), counted in characters
// rather than bytes so a truncated name is still valid UTF-8.
const (
	maxNameLength    = 50
	maxEmailLength   = 50
	maxWebsiteLength = 255
)

// The three messages this package owns. BlogCFC's bundle has no key for
// any of them: the spam refusal was a literal in addcomment.cfm, and the
// two confirmations replace a `window.close()` that said nothing at all.
const (
	spamRefused      = "Your comment has been flagged as spam."
	challengeMissing = "Please answer the question."
	challengeWrong   = "The answer to the question was not right."
	commentThanks    = "Thank you. Your comment has been added."
	commentHeld      = "Thank you. Your comment has been received and will appear once it has been approved."
	subscribedThanks = "You are subscribed. Any new comments on this entry will be sent to your address."
	commentKilled    = "The comment has been deleted."
	commentApproved  = "The comment has been approved."
	commentGone      = "That comment is no longer there."
	challengePrompt  = "Answer this question to prove you are human:"
	postCommentLabel = "Post Comment"
)

// The antispam form fields this package adds on top of the two the
// antispam package names: the challenge's answer and the token that
// carries its signature (PLAN §7, `usecaptcha`).
const (
	challengeAnswerField = "captcha"
	challengeTokenField  = "captchatoken"
)

// commentPages are this package's standalone documents. Like the print
// view they are whole pages with no site layout: BlogCFC opened them in a
// popup window, and `body#popUpFormBody` is what the stylesheet expects
// (PLAN §12 "Popups"). They render full-page just as well.
var commentPages = []string{"comment_form.html", "comment_subscribe.html", "comment_done.html"}

// commentTemplates is parsed once at start; a template that does not
// parse is a build-time mistake, as it is in New.
var commentTemplates = func() map[string]*template.Template {
	out := make(map[string]*template.Template, len(commentPages))
	for _, name := range commentPages {
		out[name] = template.Must(template.ParseFS(templateFS, "templates/"+name))
	}
	return out
}()

// renderComment writes one of those documents, through a buffer so a
// template error cannot leave half a page on the wire.
func (m *Module) renderComment(w http.ResponseWriter, page string, status int, data any) {
	t, ok := commentTemplates[page]
	if !ok {
		slog.Error("web: unknown comment template", "page", page)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, page, data); err != nil {
		slog.Error("web: render failed", "page", page, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if _, err := buf.WriteTo(w); err != nil {
		slog.Debug("web: write failed", "error", err)
	}
}

// commentFormPage is /comments/add/{id}.
type commentFormPage struct {
	Title   string
	CSSURL  string
	Heading string
	Legend  string

	ActionURL   string
	SubmitLabel string

	AllowComments bool
	NotAllowed    string

	ErrorHeading string
	Errors       []string

	Name      string
	Email     string
	Website   string
	Comment   string
	Subscribe bool
	Remember  bool

	NameLabel      string
	EmailLabel     string
	WebsiteLabel   string
	CommentLabel   string
	SubscribeLabel string
	SubscribeText  string
	RememberLabel  string

	// The antispam fields: the honeypot that must stay empty, the signed
	// timestamp, and the arithmetic challenge when `usecaptcha` asks for
	// one (PLAN §11 "Antispam").
	HoneypotField  string
	StampField     string
	StampValue     string
	Challenge      string
	ChallengeToken string
	ChallengeLabel string
	AnswerField    string
	TokenField     string
}

// commentSubscribePage is /comments/subscribe/{id}: addsub.cfm, which is
// the comment form with only an address on it (PLAN §9 C09).
type commentSubscribePage struct {
	Title   string
	CSSURL  string
	Heading string
	Legend  string

	ActionURL   string
	SubmitLabel string

	AllowComments bool
	NotAllowed    string

	ErrorHeading string
	Errors       []string

	Email      string
	EmailLabel string
}

// commentDonePage is the small page that ends a popup: the thank-you
// after a comment or a subscription, and the confirmation the owner's
// one-click Delete and Approve links answer with.
type commentDonePage struct {
	BodyID  string
	Title   string
	CSSURL  string
	Heading string
	Message string

	LinkURL   string
	LinkLabel string

	// ClosePopup reloads the window this one was opened from and closes
	// itself, which is how addcomment.cfm ended. Full-page, nothing
	// happens and the link above is what the reader follows.
	ClosePopup bool
}

// spamCheck is the configured antispam tests, keyed with the secret the
// admin session and the visitor cookie already sign with. The word and
// IP lists are left off it here: addComment applied those itself, for
// every comment, whether or not cfFormProtect was switched on, so they
// are run separately (blog.cfc addComment, PLAN §9 C04).
func (m *Module) spamCheck() antispam.Check {
	c := antispam.Defaults()
	c.Secret = []byte(m.cfg.SessionSecret)
	return c
}

// notifier builds the comment notifier for this request. It is made per
// call rather than held on the Module: everything it needs is already on
// the Module, and the M2 seams (Module.Mail) stay as they were.
func (m *Module) notifier() *notify.Notifier {
	n := notify.New(m.cfg, m.store, m.settings, m.Mail)
	base, loc := m.base(), m.loc()
	n.Link = func(e store.Entry) string { return EntryURL(base, e, loc) }
	return n
}

// commentEntry reads the entry a comment form hangs off. An id nobody
// has, and an entry no visitor may see, are both 404 (PLAN §9 C03);
// addcomment.cfm closed its own window instead, which a full-page
// request cannot do.
func (m *Module) commentEntry(w http.ResponseWriter, r *http.Request) *store.Entry {
	e, err := m.store.GetEntry(r.Context(), strings.TrimSpace(r.PathValue("id")))
	if errors.Is(err, store.ErrNotFound) {
		m.notFound(w, r)
		return nil
	}
	if err != nil {
		m.serverError(w, r, err)
		return nil
	}
	if !e.Live(time.Now()) && !m.adminView(r) {
		m.notFound(w, r)
		return nil
	}
	return e
}

// handleAddComment is `GET|POST /comments/add/{id}`: addcomment.cfm.
func (m *Module) handleAddComment(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	e := m.commentEntry(w, r)
	if e == nil {
		return
	}
	if r.Method == http.MethodPost && e.AllowComments {
		m.postComment(w, r, e)
		return
	}
	m.renderComment(w, "comment_form.html", http.StatusOK, m.commentForm(r, e, nil))
}

// commentForm builds the form page: the submitted values on a POST that
// came back with errors, the remember-me cookies on a first visit
// (PLAN §9 C07), and a fresh honeypot, timestamp and challenge every
// time -- a re-rendered form must not reuse a stamp the visitor has
// already spent.
func (m *Module) commentForm(r *http.Request, e *store.Entry, errs []string) commentFormPage {
	page := commentFormPage{
		Title:          m.settings.BlogTitle() + " : " + m.bundle.T("addcomments"),
		CSSURL:         m.base() + "/static/css/site.css",
		Heading:        m.bundle.T("comments") + ": " + e.Title,
		Legend:         m.bundle.T("postyourcomments"),
		ActionURL:      m.base() + "/comments/add/" + url.PathEscape(e.ID),
		SubmitLabel:    postCommentLabel,
		AllowComments:  e.AllowComments,
		NotAllowed:     m.bundle.T("commentsnotallowed"),
		ErrorHeading:   m.bundle.T("correctissues"),
		Errors:         errs,
		NameLabel:      m.bundle.T("name"),
		EmailLabel:     m.bundle.T("emailaddress"),
		WebsiteLabel:   m.bundle.T("website"),
		CommentLabel:   m.bundle.T("comments"),
		SubscribeLabel: m.bundle.T("subscribe"),
		SubscribeText:  m.bundle.T("subscribetext"),
		RememberLabel:  m.bundle.T("remembermyinfo"),
		AnswerField:    challengeAnswerField,
		TokenField:     challengeTokenField,
		ChallengeLabel: challengePrompt,
	}

	if r.Method == http.MethodPost {
		page.Name = formValue(r, "name")
		page.Email = formValue(r, "email")
		page.Website = formValue(r, "website")
		page.Comment = strings.TrimSpace(r.FormValue("comment"))
		page.Subscribe = checked(r, "subscribe")
		page.Remember = checked(r, "remember")
	} else {
		page.Name = cookieValue(r, cookieName)
		page.Email = cookieValue(r, cookieEmail)
		page.Website = cookieValue(r, cookieWebsite)
		page.Remember = page.Name != "" || page.Email != "" || page.Website != ""
	}
	if page.Website == "" {
		page.Website = websitePlaceholder
	}

	check := m.spamCheck()
	page.HoneypotField, page.StampField, page.StampValue = check.Fields(time.Now())
	if m.challengeWanted(r) {
		page.Challenge, page.ChallengeToken = antispam.Challenge(check.Secret, time.Now())
	}
	return page
}

// challengeWanted reports whether this visitor has to answer the
// arithmetic question: `usecaptcha` is on and nobody is logged in
// (addcomment.cfm's `application.useCaptcha and not isLoggedIn()`).
func (m *Module) challengeWanted(r *http.Request) bool {
	return m.settings.UseCaptcha() && m.currentUser(r) == nil
}

// postComment is the pipeline of PLAN §11, in its order: the word and IP
// lists, then cfFormProtect's tests and the challenge, then the field
// validation, then the insert, the retro-clear, the cookies and the mail.
func (m *Module) postComment(w http.ResponseWriter, r *http.Request, e *store.Entry) {
	ctx := r.Context()
	name := formValue(r, "name")
	email := formValue(r, "email")
	website := formValue(r, "website")
	comment := strings.TrimSpace(r.FormValue("comment"))
	if website == websitePlaceholder {
		website = ""
	}
	subscribe := checked(r, "subscribe")
	remember := checked(r, "remember")
	loggedIn := m.currentUser(r) != nil

	// The two lists blog.cfc refused on outright, before anything else
	// and whatever cfFormProtect thought (PLAN §9 C04). The texts are
	// addComment's own four, in its order.
	texts := []string{comment, name, website, email}
	if word := antispam.MatchWord(m.settings.TrackbackSpamList(), texts...); word != "" {
		slog.Info("web: comment refused", "entry", e.ID, "reason", antispam.ReasonSpamWord, "word", word)
		m.commentFormError(w, r, e, spamRefused)
		return
	}
	if antispam.MatchIP(m.settings.IPBlockList(), requestIP(r)) {
		slog.Info("web: comment refused", "entry", e.ID, "reason", antispam.ReasonBlockedIP)
		m.commentFormError(w, r, e, spamRefused)
		return
	}

	// cfFormProtect's own tests, which the as-is skipped for a logged-in
	// author and for a blog with `usecfp` off (PLAN §9 C05).
	if m.settings.UseCFP() && !loggedIn {
		res := m.spamCheck().Verify(r.Form, time.Now(), requestIP(r), texts...)
		if res.Blocked {
			slog.Info("web: comment refused", "entry", e.ID, "reasons", res.Reasons, "points", res.Points)
			m.commentFormError(w, r, e, spamRefused)
			return
		}
	}

	var errs []string
	if m.challengeWanted(r) {
		answer := formValue(r, challengeAnswerField)
		token := formValue(r, challengeTokenField)
		switch {
		case answer == "":
			errs = append(errs, challengeMissing)
		case !antispam.VerifyChallenge([]byte(m.cfg.SessionSecret), token, answer, time.Now()):
			errs = append(errs, challengeWrong)
		}
	}

	// C01: addcomment.cfm's four checks, with its bundle's messages.
	if name == "" {
		errs = append(errs, m.bundle.T("mustincludename"))
	}
	if !looksLikeEmail(email) {
		errs = append(errs, m.bundle.T("mustincludeemail"))
	}
	if website != "" && externalURL(website) == "" {
		errs = append(errs, m.bundle.T("invalidurl"))
	}
	if comment == "" {
		errs = append(errs, m.bundle.T("mustincludecomments"))
	}
	if len(errs) > 0 {
		m.renderComment(w, "comment_form.html", http.StatusOK, m.commentForm(r, e, errs))
		return
	}

	// C02: the as-is's column widths, applied after validation as the
	// `left()` calls on the addComment invoke were.
	c := store.Comment{
		EntryID:   e.ID,
		Name:      truncate(name, maxNameLength),
		Email:     truncate(email, maxEmailLength),
		Website:   truncate(website, maxWebsiteLength),
		Comment:   comment,
		Subscribe: subscribe,
		// C06: a blog that moderates holds every comment but its own
		// author's (blog.cfc's `overridemoderation`).
		Moderated: !m.settings.Moderate() || loggedIn,
	}
	if err := m.store.CreateComment(ctx, &c); err != nil {
		m.serverError(w, r, err)
		return
	}

	// C08: unticking the box clears this address's earlier subscriptions
	// on the entry, the new row included.
	if !subscribe {
		if err := m.store.ClearSubscriptions(ctx, c.EntryID, c.Email); err != nil {
			slog.Error("web: clear subscriptions failed", "entry", e.ID, "error", err)
		}
	}

	m.rememberCommenter(w, remember, name, email, website)

	// C10: a held comment is the owner's business alone until it is
	// approved; a live one goes to the thread as well.
	if _, err := m.notifier().Comment(ctx, e, &c, !c.Moderated); err != nil {
		slog.Error("web: comment notification failed", "entry", e.ID, "comment", c.ID, "error", err)
	}

	link := EntryURL(m.base(), *e, m.loc()) + "#c" + c.ID
	message := commentThanks
	if !c.Moderated {
		message = commentHeld
	}
	m.renderComment(w, "comment_done.html", http.StatusOK, commentDonePage{
		BodyID:     "popUpFormBody",
		Title:      m.settings.BlogTitle() + " : " + m.bundle.T("addcomments"),
		CSSURL:     m.base() + "/static/css/site.css",
		Heading:    m.bundle.T("comments") + ": " + e.Title,
		Message:    message,
		LinkURL:    link,
		LinkLabel:  e.Title,
		ClosePopup: true,
	})
}

// commentFormError re-renders the form with one message on it.
func (m *Module) commentFormError(w http.ResponseWriter, r *http.Request, e *store.Entry, msg string) {
	m.renderComment(w, "comment_form.html", http.StatusOK, m.commentForm(r, e, []string{msg}))
}

// rememberCommenter writes, or expires, BlogCFC's three cookies
// (PLAN §9 C07). The values are the raw text: addcomment.cfm stored them
// `htmlEditFormat`ed, so a name with an apostrophe came back as
// `&#39;` in the box next time.
func (m *Module) rememberCommenter(w http.ResponseWriter, remember bool, name, email, website string) {
	secure := strings.HasPrefix(strings.ToLower(m.cfg.BlogBaseURL), "https://")
	for _, c := range []struct{ name, value string }{
		{cookieName, name}, {cookieEmail, email}, {cookieWebsite, website},
	} {
		cookie := &http.Cookie{
			Name:     c.name,
			Value:    c.value,
			Path:     "/",
			Secure:   secure,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   int(rememberLifetime / time.Second),
		}
		if !remember {
			cookie.Value = ""
			cookie.MaxAge = -1
		}
		http.SetCookie(w, cookie)
	}
}

// handleSubscribeComment is `GET|POST /comments/subscribe/{id}`:
// addsub.cfm, a thread subscription with no comment attached. The row is
// a comment row with `subscribeonly` set, which is how BlogCFC stored it
// and why the store keeps such rows out of every listing and count
// (PLAN §9 C09).
func (m *Module) handleSubscribeComment(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	e := m.commentEntry(w, r)
	if e == nil {
		return
	}
	page := commentSubscribePage{
		Title:         m.settings.BlogTitle() + " : " + m.bundle.T("addsub"),
		CSSURL:        m.base() + "/static/css/site.css",
		Heading:       m.bundle.T("comments") + ": " + e.Title,
		Legend:        m.bundle.T("addsub"),
		ActionURL:     m.base() + "/comments/subscribe/" + url.PathEscape(e.ID),
		SubmitLabel:   m.bundle.T("subscribe"),
		AllowComments: e.AllowComments,
		NotAllowed:    m.bundle.T("subnotallowed"),
		ErrorHeading:  m.bundle.T("correctissues"),
		EmailLabel:    m.bundle.T("emailaddress"),
	}
	if r.Method != http.MethodPost || !e.AllowComments {
		if r.Method != http.MethodPost {
			page.Email = cookieValue(r, cookieEmail)
		}
		m.renderComment(w, "comment_subscribe.html", http.StatusOK, page)
		return
	}

	page.Email = formValue(r, "email")
	if !looksLikeEmail(page.Email) {
		page.Errors = append(page.Errors, m.bundle.T("mustincludeemail"))
		m.renderComment(w, "comment_subscribe.html", http.StatusOK, page)
		return
	}

	// addsub.cfm called addComment with an empty comment, `subscribe` and
	// `subscribeonly` both true, and no spam checks of any kind.
	c := store.Comment{
		EntryID:       e.ID,
		Email:         truncate(page.Email, maxEmailLength),
		Subscribe:     true,
		SubscribeOnly: true,
		// A subscription is nothing to moderate: it shows nowhere, so
		// holding it would only fill the queue (blog.cfc stored it
		// unmoderated and no page ever looked).
		Moderated: true,
	}
	if err := m.store.CreateComment(r.Context(), &c); err != nil {
		m.serverError(w, r, err)
		return
	}

	m.renderComment(w, "comment_done.html", http.StatusOK, commentDonePage{
		BodyID:     "popUpFormBody",
		Title:      m.settings.BlogTitle() + " : " + m.bundle.T("addsub"),
		CSSURL:     m.base() + "/static/css/site.css",
		Heading:    m.bundle.T("comments") + ": " + e.Title,
		Message:    subscribedThanks,
		LinkURL:    EntryURL(m.base(), *e, m.loc()),
		LinkLabel:  e.Title,
		ClosePopup: true,
	})
}

// killComment answers `/?killcomment={token}` (PLAN §9 C11): the owner's
// one-click Delete link from a notification mail. The token is the only
// credential, as it is in the as-is -- BlogApplication.cfc ran this in
// onRequestStart, for anybody holding the link, logged in or not.
func (m *Module) killComment(w http.ResponseWriter, r *http.Request, token string) {
	message := commentKilled
	c, err := m.store.GetCommentByKillToken(r.Context(), token)
	switch {
	case errors.Is(err, store.ErrNotFound):
		message = commentGone
	case err != nil:
		m.serverError(w, r, err)
		return
	default:
		if err := m.store.DeleteComments(r.Context(), []string{c.ID}); err != nil {
			m.serverError(w, r, err)
			return
		}
	}
	m.commentAction(w, r, message)
}

// approveComment answers `/?approvecomment={id}`: the owner's one-click
// Approve link, which only appears while moderation is on.
func (m *Module) approveComment(w http.ResponseWriter, r *http.Request, id string) {
	message := commentApproved
	err := m.store.ApproveComment(r.Context(), id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		message = commentGone
	case err != nil:
		m.serverError(w, r, err)
		return
	}
	m.commentAction(w, r, message)
}

// commentAction is the page the two one-click links answer with. The
// as-is did the work in onRequestStart and then rendered the home page,
// which said nothing about what had happened; a line saying so, with the
// way home under it, is what the owner clicking from their mail wants.
func (m *Module) commentAction(w http.ResponseWriter, r *http.Request, message string) {
	m.renderComment(w, "comment_done.html", http.StatusOK, commentDonePage{
		BodyID:    "commentActionBody",
		Title:     m.settings.BlogTitle() + " : " + m.bundle.T("comments"),
		CSSURL:    m.base() + "/static/css/site.css",
		Heading:   m.bundle.T("comments"),
		Message:   message,
		LinkURL:   m.base() + "/",
		LinkLabel: m.settings.BlogTitle(),
	})
}

// formAntispam is the antispam block the contact and send forms carry,
// and the tests they have to pass. BlogCFC ran cfFormProtect and the
// captcha on both of those pages too (contact.cfm, send.cfm), with one
// difference from the comment form: neither page let a logged-in author
// past, because neither asked. Skipping them for a logged-in author is
// this rewrite's (PLAN §11 "Antispam": the checks are for visitors).
type formAntispam struct {
	HoneypotField  string
	StampField     string
	StampValue     string
	Challenge      string
	ChallengeToken string
	ChallengeLabel string
	AnswerField    string
	TokenField     string
}

// antispamFields builds a fresh block for a form about to be rendered.
func (m *Module) antispamFields(r *http.Request) formAntispam {
	check := m.spamCheck()
	f := formAntispam{
		ChallengeLabel: challengePrompt,
		AnswerField:    challengeAnswerField,
		TokenField:     challengeTokenField,
	}
	f.HoneypotField, f.StampField, f.StampValue = check.Fields(time.Now())
	if m.challengeWanted(r) {
		f.Challenge, f.ChallengeToken = antispam.Challenge(check.Secret, time.Now())
	}
	return f
}

// antispamErrors runs the whole defence over a posted form and returns
// what a visitor should be told: one line for a submission cfFormProtect
// or the word list refused, and the challenge's own two messages. It is
// the comment pipeline's checks in one call, for the pages whose order
// has nothing else in it. Nothing runs for a logged-in author.
func (m *Module) antispamErrors(r *http.Request, texts ...string) []string {
	if m.currentUser(r) != nil {
		return nil
	}
	var errs []string
	if word := antispam.MatchWord(m.settings.TrackbackSpamList(), texts...); word != "" {
		slog.Info("web: form refused", "path", r.URL.Path, "reason", antispam.ReasonSpamWord, "word", word)
		return []string{spamRefused}
	}
	if m.settings.UseCFP() {
		res := m.spamCheck().Verify(r.Form, time.Now(), requestIP(r), texts...)
		if res.Blocked {
			slog.Info("web: form refused", "path", r.URL.Path, "reasons", res.Reasons, "points", res.Points)
			return []string{spamRefused}
		}
	}
	if m.settings.UseCaptcha() {
		answer := formValue(r, challengeAnswerField)
		token := formValue(r, challengeTokenField)
		switch {
		case answer == "":
			errs = append(errs, challengeMissing)
		case !antispam.VerifyChallenge([]byte(m.cfg.SessionSecret), token, answer, time.Now()):
			errs = append(errs, challengeWrong)
		}
	}
	return errs
}

// checked reads a checkbox the way CFML's isBoolean did: anything the
// browser sends for a ticked box is true, an absent field is false.
func checked(r *http.Request, name string) bool {
	switch strings.ToLower(formValue(r, name)) {
	case "", "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

// cookieValue reads one cookie, trimmed, or "".
func cookieValue(r *http.Request, name string) string {
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(c.Value)
}

// truncate is CFML's left(): a count of characters, not of bytes.
func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}
