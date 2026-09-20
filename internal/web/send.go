package web

import (
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/4scottt/go-blogcfc/internal/mail"
	"github.com/4scottt/go-blogcfc/internal/render"
	"github.com/4scottt/go-blogcfc/internal/store"
)

// errNoMailSender and errNoOwnerEmail are the two ways a form that has
// passed validation can still fail to send: the process has no sender
// wired (main.go always gives it one; a test may not) or the blog has no
// owner address to send to.
var (
	errNoMailSender = errors.New("web: no mail sender configured")
	errNoOwnerEmail = errors.New("web: the blog has no owneremail setting")
)

// sendPage is /send/{id}.
type sendPage struct {
	pageData
	formAntispam

	Heading     string
	Intro       string
	ActionURL   string
	SubmitLabel string

	ErrorHeading string
	Errors       []string

	Email  string
	REmail string
	Notes  string

	EmailLabel  string
	REmailLabel string
	NotesLabel  string

	Sent        bool
	SentMessage string
}

// sendMailBody is send.cfm's message, which is HTML: who sent it, which
// blog it came from, the entry's title and permalink, the optional notes
// and then the entry itself.
var sendMailBody = template.Must(template.New("sendmail").Parse(`<p>
The following blog entry was sent to you from: <b>{{.From}}</b><br />
It came from the blog: <b>{{.BlogTitle}}</b><br />
The entry is titled: <b>{{.Title}}</b><br />
The entry can be found here: <b><a href="{{.Link}}">{{.Link}}</a></b>
</p>
{{if .Notes}}<p>
The following notes were included:<br />
<b>{{.Notes}}</b>
</p>
<hr />
{{end}}{{.Body}}{{.MoreBody}}
`))

// handleSend is `GET|POST /send/{id}`: BlogCFC's send.cfm, which mails an
// entry to a friend and copies the blog's owner (PLAN §9 P22). The field
// names are the as-is ones and stay: `email` is the sender, `remail` the
// recipient, `notes` the optional message, `send` the submit.
//
// An unknown entry, or one that is not live, is a 404. send.cfm bounced
// both to the home page; a missing thing is a 404 here (PLAN §8).
func (m *Module) handleSend(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	e, err := m.store.GetEntry(r.Context(), strings.TrimSpace(r.PathValue("id")))
	if errors.Is(err, store.ErrNotFound) {
		m.notFound(w, r)
		return
	}
	if err != nil {
		m.serverError(w, r, err)
		return
	}
	if !e.Live(time.Now()) && !m.adminView(r) {
		m.notFound(w, r)
		return
	}

	page := sendPage{
		pageData:     m.newPage(r, m.bundle.T("send")),
		Heading:      m.bundle.T("sendentry") + ": " + e.Title,
		Intro:        m.bundle.T("sendform"),
		ActionURL:    m.base() + "/send/" + e.ID,
		SubmitLabel:  m.bundle.T("sendentry"),
		ErrorHeading: m.bundle.T("correctissues"),
		Email:        formValue(r, "email"),
		REmail:       formValue(r, "remail"),
		Notes:        formValue(r, "notes"),
		EmailLabel:   m.bundle.T("youremailaddress"),
		REmailLabel:  m.bundle.T("receiveremailaddress"),
		NotesLabel:   m.bundle.T("optionalnotes"),
		formAntispam: m.antispamFields(r),
	}

	if r.Method != http.MethodPost && !r.Form.Has("send") {
		m.renderExtra(w, "send.html", http.StatusOK, page)
		return
	}

	if !looksLikeEmail(page.Email) {
		page.Errors = append(page.Errors, m.bundle.T("mustincludeemail"))
	}
	if !looksLikeEmail(page.REmail) {
		page.Errors = append(page.Errors, m.bundle.T("mustincludereceiveremail"))
	}
	// The antispam block, as on the contact form (PLAN §11 "Antispam").
	page.Errors = append(page.Errors, m.antispamErrors(r, page.Notes, page.Email, page.REmail)...)

	if len(page.Errors) > 0 {
		m.renderExtra(w, "send.html", http.StatusOK, page)
		return
	}

	if err := m.sendEntryMail(r, e, page.Email, page.REmail, page.Notes); err != nil {
		slog.Error("web: send entry mail", "entry", e.ID, "error", err)
		page.Errors = append(page.Errors, mailFailed)
		m.renderExtra(w, "send.html", http.StatusOK, page)
		return
	}

	page.Sent = true
	page.SentMessage = m.bundle.T("entrysent")
	m.renderExtra(w, "send.html", http.StatusOK, page)
}

// renderOrEmpty keeps an entry without a morebody from growing an empty
// paragraph at the end of the mail.
func renderOrEmpty(body string, opts render.Options) template.HTML {
	if strings.TrimSpace(body) == "" {
		return ""
	}
	return render.Entry(body, opts)
}

// sendEntryMail builds and sends send.cfm's message.
func (m *Module) sendEntryMail(r *http.Request, e *store.Entry, from, to, notes string) error {
	if m.Mail == nil {
		return errNoMailSender
	}
	base := m.base()
	link := EntryURL(base, *e, m.loc())
	opts := render.Options{
		Enclosure:    e.Enclosure,
		EnclosureURL: downloadURL(base, *e),
		MimeType:     e.MimeType,
	}
	var body strings.Builder
	err := sendMailBody.Execute(&body, struct {
		From      string
		BlogTitle string
		Title     string
		Link      string
		Notes     string
		Body      template.HTML
		MoreBody  template.HTML
	}{
		From:      from,
		BlogTitle: m.settings.BlogTitle(),
		Title:     e.Title,
		Link:      link,
		Notes:     notes,
		Body:      render.Entry(e.Body, opts),
		MoreBody:  renderOrEmpty(e.MoreBody, opts),
	})
	if err != nil {
		return err
	}

	msg := mail.Message{
		To:      []string{to},
		From:    m.mailFrom(),
		Subject: m.bundle.T("blogentryfrom") + ": " + m.settings.BlogTitle(),
		Body:    body.String(),
		HTML:    true,
	}
	if owner := strings.TrimSpace(m.settings.OwnerEmail()); owner != "" {
		msg.Cc = []string{owner}
	}
	return m.Mail.Send(r.Context(), msg)
}
