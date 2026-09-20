package admin_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/4scottt/go-blogcfc/internal/mail"
)

// TestFP_A16_MailAllVerifiedSubscribersWithUnsubscribeBlock covers PLAN
// §9 A16: one message per verified subscriber, each carrying that
// recipient's own unsubscribe link (admin/mailsubscribers.cfm).
func TestFP_A16_MailAllVerifiedSubscribersWithUnsubscribeBlock(t *testing.T) {
	h := newHarness(t)
	h.user("admin", "Admin")
	h.setSetting("owneremail", "owner@example.com")

	ada := h.subscribe("ada@example.com", true)
	bob := h.subscribe("bob@example.com", true)
	h.subscribe("cyd@example.com", false)

	sent := &mail.Recorder{}
	h.module.Mail = sent

	h.login("admin")
	resp, body := h.get("/admin/subscribers/mail")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mail subscribers: status %d, want 200", resp.StatusCode)
	}
	for _, want := range []string{
		"<h1>Mail Subscribers</h1>",
		`name="subject"`, `name="body"`, `name="html"`, `value="Send"`,
		`action="/admin/subscribers/mail"`,
		"2 verified",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the mail form is missing %s", want)
		}
	}

	// A blank subject is refused and nothing goes out.
	resp, body = h.postForm("/admin/subscribers/mail", url.Values{
		"subject": {"  "}, "body": {"Hello"}, "send": {"Send"}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("blank subject: status %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "The subject cannot be blank.") {
		t.Error("a blank subject is not refused")
	}
	if len(sent.Messages()) != 0 {
		t.Fatal("a refused broadcast sent mail anyway")
	}

	resp, body = h.postForm("/admin/subscribers/mail", url.Values{
		"subject": {"News from the blog"}, "body": {"<p>Hello, subscribers.</p>"},
		"html": {"yes"}, "send": {"Send"}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("send: status %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "Your message was sent to 2 subscribers.") {
		t.Error("the result page does not say how many were mailed")
	}

	msgs := sent.Messages()
	if len(msgs) != 2 {
		t.Fatalf("%d messages sent, want one per verified subscriber", len(msgs))
	}
	base := "http://127.0.0.1:8080"
	for i, want := range []struct{ email, token string }{
		{ada.Email, ada.Token}, {bob.Email, bob.Token},
	} {
		m := msgs[i]
		if len(m.To) != 1 || m.To[0] != want.email {
			t.Errorf("message %d went to %v, want just %s", i, m.To, want.email)
		}
		if m.From != "owner@example.com" {
			t.Errorf("message %d is from %q, want the blog's owner", i, m.From)
		}
		if m.Subject != "News from the blog" {
			t.Errorf("message %d has subject %q", i, m.Subject)
		}
		if !m.HTML {
			t.Errorf("message %d is not HTML although the box was ticked", i)
		}
		if !strings.Contains(m.Body, "<p>Hello, subscribers.</p>") {
			t.Errorf("message %d does not carry the body that was typed", i)
		}
		unsub := base + "/unsubscribe?email=" + url.QueryEscape(want.email) + "&token=" + url.QueryEscape(want.token)
		if !strings.Contains(m.Body, unsub) {
			t.Errorf("message %d has no unsubscribe link for %s\nbody: %s", i, want.email, m.Body)
		}
		if strings.Contains(m.Body, "cyd@example.com") {
			t.Errorf("message %d carries another subscriber's address", i)
		}
	}
	if strings.Contains(msgs[0].Body, bob.Token) {
		t.Error("the first message carries the second subscriber's token: the block is not per recipient")
	}
}
