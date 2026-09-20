package admin

import (
	"net/http"
	"strings"

	"github.com/4scottt/go-blogcfc/internal/mail"
)

// mailSubscribersPage is mailsubscribers.html's own data.
type mailSubscribersPage struct {
	Errors []string

	Subject string
	Body    string
	HTML    bool

	// Verified is how many addresses the message would go to; Sent is how
	// many it went to, once Done.
	Verified int
	Sent     int
	Done     bool
}

// mailSubscribersForm is GET /admin/subscribers/mail (PLAN §9 A16,
// admin/mailsubscribers.cfm).
func (m *Module) mailSubscribersForm(w http.ResponseWriter, r *http.Request) {
	n, err := m.verifiedCount(r)
	if err != nil {
		m.serverError(w, r, err)
		return
	}
	m.renderMailSubscribers(w, r, mailSubscribersPage{Verified: n})
}

// mailSubscribersSend is POST /admin/subscribers/mail: one message per
// verified subscriber, each with its own unsubscribe block, as the as-is
// page's loop built (PLAN §9 A16).
func (m *Module) mailSubscribersSend(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	ctx := r.Context()

	p := mailSubscribersPage{
		Subject: strings.TrimSpace(r.PostFormValue("subject")),
		Body:    strings.TrimSpace(r.PostFormValue("body")),
		HTML:    r.PostFormValue("html") != "",
	}
	p.Subject = clip(p.Subject, 255)

	subs, err := m.store.VerifiedSubscribers(ctx)
	if err != nil {
		m.serverError(w, r, err)
		return
	}
	p.Verified = len(subs)

	if p.Subject == "" {
		p.Errors = append(p.Errors, "The subject cannot be blank.")
	}
	if p.Body == "" {
		p.Errors = append(p.Errors, "The body cannot be blank.")
	}
	if m.Mail == nil {
		p.Errors = append(p.Errors, "This blog has no mail sender configured.")
	}
	if len(p.Errors) > 0 {
		m.renderMailSubscribers(w, r, p)
		return
	}

	from := strings.TrimSpace(m.settings.OwnerEmail())
	base := m.settings.BlogURL()
	for _, s := range subs {
		msg := mail.MailAllSubscribers(mail.MailAllSubscribersVars{
			BaseURL: base,
			Subject: p.Subject,
			Body:    p.Body,
			From:    from,
			Email:   s.Email,
			Token:   s.Token,
			HTML:    p.HTML,
		})
		if err := m.Mail.Send(ctx, msg); err != nil {
			m.serverError(w, r, err)
			return
		}
		p.Sent++
	}
	p.Done = true
	m.renderMailSubscribers(w, r, p)
}

// verifiedCount is how many addresses a broadcast would reach.
func (m *Module) verifiedCount(r *http.Request) (int, error) {
	subs, err := m.store.VerifiedSubscribers(r.Context())
	if err != nil {
		return 0, err
	}
	return len(subs), nil
}

func (m *Module) renderMailSubscribers(w http.ResponseWriter, r *http.Request, p mailSubscribersPage) {
	data := m.newPageData(r, "Mail Subscribers")
	data.Page = p
	render(w, "mailsubscribers.html", data)
}
