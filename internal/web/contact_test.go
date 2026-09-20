package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestFP_P21_ContactValidatesAndMailsOwnerWithRemoteAddress is P21: the
// three required fields with the bundle's messages, and a mail to the
// blog's owner carrying the name, the address, the comments and the
// visitor's remote address (PLAN §9 P21, contact.cfm).
func TestFP_P21_ContactValidatesAndMailsOwnerWithRemoteAddress(t *testing.T) {
	s := newExtraSite(t)
	s.setSetting("owneremail", "owner@example.com")
	// The arithmetic challenge is `usecaptcha`, which contact.cfm asked
	// for too; this test posts without going through the form, so it is
	// off here. TestFP_C05 covers the antispam block on this page.
	s.setSetting("usecaptcha", "no")

	// The form itself: every control named, the submit's own label.
	form := s.getOK("/contact")
	mustContain(t, "the contact form", form,
		`<form action="`+testBase+`/contact" method="post"`,
		`<input type="text" id="name" name="name"`,
		`<input type="text" id="email" name="email"`,
		`<textarea id="comments" name="comments"`,
		`<input type="submit" id="submit" name="send" value="Send Your Comments" />`,
		"Contact Blog Owner",
	)

	// Nothing filled in: the three messages, no mail.
	rec := s.post("/contact", url.Values{"send": {"Send Your Comments"}}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("an invalid contact form = %d, want 200 with the errors", rec.Code)
	}
	mustContain(t, "the validation errors", rec.Body.String(),
		"Please correct the following issue(s)",
		"You must include your name.",
		"You must include a valid email address.",
		"You must include your comments.",
	)
	if n := len(s.mail.Messages()); n != 0 {
		t.Fatalf("an invalid form sent %d messages", n)
	}

	// A bad address alone still fails, and what was typed comes back.
	rec = s.post("/contact", url.Values{
		"send": {"Send Your Comments"}, "name": {"Ray"}, "email": {"ray-at-example"}, "comments": {"Hello."},
	}, nil)
	body := rec.Body.String()
	mustContain(t, "the address error", body, "You must include a valid email address.", `value="Ray"`)
	if strings.Contains(body, "You must include your name.") {
		t.Error("a filled-in name was still reported missing")
	}
	if n := len(s.mail.Messages()); n != 0 {
		t.Fatalf("a bad address sent %d messages", n)
	}

	// A good form: the thank-you text and one mail to the owner.
	rec = s.post("/contact", url.Values{
		"send": {"Send Your Comments"}, "name": {"Ray"}, "email": {"ray@camdenfamily.com"},
		"comments": {"Nice blog you have here."},
	}, map[string]string{"X-Forwarded-For": "203.0.113.9, 10.0.0.1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("a valid contact form = %d, want 200", rec.Code)
	}
	mustContain(t, "the thank-you page", rec.Body.String(),
		"Your comments have been sent to the blog owner.")
	if strings.Contains(rec.Body.String(), `name="comments"`) {
		t.Error("the form is still on the page after a successful send")
	}

	msgs := s.mail.Messages()
	if len(msgs) != 1 {
		t.Fatalf("sent %d messages, want one", len(msgs))
	}
	m := msgs[0]
	if len(m.To) != 1 || m.To[0] != "owner@example.com" {
		t.Errorf("the mail went to %v, want the owner", m.To)
	}
	if m.From != s.settings.FailTo() {
		t.Errorf("From is %q; the blog's own failto address sends, never the visitor's", m.From)
	}
	if m.Subject != "Contact Form" {
		t.Errorf("subject = %q, want the bundle's contactform", m.Subject)
	}
	if m.HTML {
		t.Error("contact.cfm sent plain text")
	}
	mustContain(t, "the contact mail", m.Body,
		"Comment made by: Ray (ray@camdenfamily.com)",
		"IP of Poster: 203.0.113.9",
		"Nice blog you have here.",
		"Comment added: ",
	)

	// The envelope sender follows `failto`, never the visitor: From with
	// a stranger's domain is what modern receivers refuse.
	s.mail.Reset()
	s.setSetting("failto", "blog@example.com")
	s.post("/contact", url.Values{
		"send": {"Send Your Comments"}, "name": {"Ray"}, "email": {"ray@camdenfamily.com"}, "comments": {"Again."},
	}, nil)
	msgs = s.mail.Messages()
	if len(msgs) != 1 || msgs[0].From != "blog@example.com" {
		t.Fatalf("From = %+v, want failto", msgs)
	}
}
