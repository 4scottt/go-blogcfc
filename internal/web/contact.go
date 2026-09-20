package web

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/4scottt/go-blogcfc/internal/i18n"
	"github.com/4scottt/go-blogcfc/internal/mail"
)

// contactPage is /contact.
type contactPage struct {
	pageData
	formAntispam

	Heading     string
	Intro       string
	ActionURL   string
	SubmitLabel string

	ErrorHeading string
	Errors       []string

	Name     string
	Email    string
	Comments string

	NameLabel     string
	EmailLabel    string
	CommentsLabel string

	Sent        bool
	SentMessage string
}

// handleContact is `GET|POST /contact`: BlogCFC's contact.cfm, a form
// that mails the blog's owner (PLAN §9 P21). The fields are the as-is
// ones - `name`, `email`, `comments`, submitted as `send` - and so are
// the three validation messages, which come from the bundle.
func (m *Module) handleContact(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	page := contactPage{
		pageData:      m.newPage(r, m.bundle.T("contactowner")),
		Heading:       m.bundle.T("contactowner"),
		Intro:         m.bundle.T("contactownerform"),
		ActionURL:     m.base() + "/contact",
		SubmitLabel:   m.bundle.T("sendcontact"),
		ErrorHeading:  m.bundle.T("correctissues"),
		Name:          formValue(r, "name"),
		Email:         formValue(r, "email"),
		Comments:      formValue(r, "comments"),
		NameLabel:     m.bundle.T("name"),
		EmailLabel:    m.bundle.T("youremailaddress"),
		CommentsLabel: m.bundle.T("comments"),
		formAntispam:  m.antispamFields(r),
	}

	if r.Method != http.MethodPost && !r.Form.Has("send") {
		m.renderExtra(w, "contact.html", http.StatusOK, page)
		return
	}

	if page.Name == "" {
		page.Errors = append(page.Errors, m.bundle.T("mustincludename"))
	}
	if !looksLikeEmail(page.Email) {
		page.Errors = append(page.Errors, m.bundle.T("mustincludeemail"))
	}
	if page.Comments == "" {
		page.Errors = append(page.Errors, m.bundle.T("mustincludecomments"))
	}
	// The antispam block (PLAN §11 "Antispam"): the word list, then
	// cfFormProtect's honeypot, timestamp and URL count, then the
	// arithmetic challenge - none of it for a logged-in author.
	page.Errors = append(page.Errors, m.antispamErrors(r, page.Comments, page.Name, page.Email)...)

	if len(page.Errors) > 0 {
		m.renderExtra(w, "contact.html", http.StatusOK, page)
		return
	}

	if err := m.sendContactMail(r, page.Name, page.Email, page.Comments); err != nil {
		slog.Error("web: contact mail", "error", err)
		page.Errors = append(page.Errors, mailFailed)
		m.renderExtra(w, "contact.html", http.StatusOK, page)
		return
	}

	page.Sent = true
	page.SentMessage = m.bundle.T("contactsent")
	m.renderExtra(w, "contact.html", http.StatusOK, page)
}

// mailFailed is what a visitor reads when the sender is unreachable.
// BlogCFC had no such case: cfmail queued to disk and always "worked".
const mailFailed = "Your message could not be sent just now. Please try again later."

// sendContactMail is contact.cfm's body: when the comment was made, who
// made it, their address, and the comments themselves.
func (m *Module) sendContactMail(r *http.Request, name, email, comments string) error {
	if m.Mail == nil {
		return errNoMailSender
	}
	owner := strings.TrimSpace(m.settings.OwnerEmail())
	if owner == "" {
		return errNoOwnerEmail
	}
	now := time.Now().In(m.loc())
	locale := m.settings.Locale()

	var b strings.Builder
	b.WriteString(m.bundle.T("commentadded") + ": " + i18n.FormatDate(locale, now, i18n.StyleLong) +
		" / " + i18n.FormatDate(locale, now, i18n.StyleTime) + "\n")
	b.WriteString(m.bundle.T("commentmadeby") + ": " + name + " (" + email + ")\n")
	b.WriteString(m.bundle.T("ipofposter") + ": " + requestIP(r) + "\n\n")
	b.WriteString(comments + "\n\n")
	b.WriteString("------------------------------------------------------------\n")
	b.WriteString("This blog powered by go-blogcfc " + Version + ", a rewrite of BlogCFC by Raymond Camden\n")

	return m.Mail.Send(r.Context(), mail.Message{
		To:      []string{owner},
		From:    m.mailFrom(),
		Subject: m.bundle.T("contactform"),
		Body:    b.String(),
	})
}
