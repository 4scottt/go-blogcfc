package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/4scottt/go-blogcfc/internal/store"
)

// TestFP_P22_SendEntryValidatesMailsRecipientCcOwner is P22: the
// email-this-entry form, its two address checks, and a mail to the
// recipient copying the owner with the entry's permalink, the notes and
// the entry itself (PLAN §9 P22, send.cfm).
func TestFP_P22_SendEntryValidatesMailsRecipientCcOwner(t *testing.T) {
	s := newExtraSite(t)
	s.setSetting("owneremail", "owner@example.com")
	s.setSetting("blogtitle", "Raymond Camden's Blog")
	s.author("ray", "Raymond Camden")
	now := time.Now().UTC().Truncate(time.Second)

	e := s.entry(store.Entry{Title: "Sendable", Alias: "sendable",
		Body: "<p>The body.</p>", MoreBody: "<p>The rest.</p>",
		Posted: utc(2026, 4, 9, 11, 5), Username: "ray", Released: true})
	draft := s.entry(store.Entry{Title: "Draft", Alias: "draft-send", Body: "<p>Not yours.</p>",
		Posted: now.Add(-time.Hour), Username: "ray"})

	// An unknown entry and a draft are both 404, never a form.
	if rec := s.get("/send/99999999-9999-4999-8999-999999999999"); rec.Code != http.StatusNotFound {
		t.Errorf("an unknown entry = %d, want 404", rec.Code)
	}
	if rec := s.get("/send/" + draft.ID); rec.Code != http.StatusNotFound {
		t.Errorf("a draft = %d, want 404", rec.Code)
	}

	// The form keeps send.cfm's field names.
	form := s.getOK("/send/" + e.ID)
	mustContain(t, "the send form", form,
		`<form action="`+testBase+`/send/`+e.ID+`" method="post"`,
		`<input type="text" id="email" name="email"`,
		`<input type="text" id="remail" name="remail"`,
		`<textarea id="notes" name="notes"`,
		`<input type="submit" id="submit" name="send" value="Send Entry" />`,
		"Send Entry: Sendable",
	)

	// Neither address given.
	rec := s.post("/send/"+e.ID, url.Values{"send": {"Send Entry"}}, nil)
	mustContain(t, "the validation errors", rec.Body.String(),
		"Please correct the following issue(s)",
		"You must include a valid email address.",
		"You must include a valid receiver email address.",
	)
	if n := len(s.mail.Messages()); n != 0 {
		t.Fatalf("an invalid form sent %d messages", n)
	}

	// The recipient's address alone is wrong.
	rec = s.post("/send/"+e.ID, url.Values{
		"send": {"Send Entry"}, "email": {"ray@camdenfamily.com"}, "remail": {"friend-at-example"},
	}, nil)
	body := rec.Body.String()
	if strings.Contains(body, "You must include a valid email address.") {
		t.Error("a good sender address was still reported invalid")
	}
	mustContain(t, "the receiver error", body, "You must include a valid receiver email address.")

	// A good form.
	rec = s.post("/send/"+e.ID, url.Values{
		"send": {"Send Entry"}, "email": {"ray@camdenfamily.com"}, "remail": {"friend@example.org"},
		"notes": {"Thought of you."},
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("a valid send form = %d, want 200", rec.Code)
	}
	mustContain(t, "the thank-you page", rec.Body.String(), "The blog entry has been sent.")

	msgs := s.mail.Messages()
	if len(msgs) != 1 {
		t.Fatalf("sent %d messages, want one", len(msgs))
	}
	m := msgs[0]
	if len(m.To) != 1 || m.To[0] != "friend@example.org" {
		t.Errorf("the mail went to %v, want the receiver", m.To)
	}
	if len(m.Cc) != 1 || m.Cc[0] != "owner@example.com" {
		t.Errorf("Cc is %v, want the blog's owner", m.Cc)
	}
	if m.Subject != "Blog entry from: Raymond Camden's Blog" {
		t.Errorf("subject = %q", m.Subject)
	}
	if !m.HTML {
		t.Error("send.cfm sent type=html")
	}
	permalink := testBase + "/2026/4/9/sendable"
	mustContain(t, "the sent mail", m.Body,
		"sent to you from: <b>ray@camdenfamily.com</b>",
		"It came from the blog: <b>Raymond Camden&#39;s Blog</b>",
		"The entry is titled: <b>Sendable</b>",
		`<a href="`+permalink+`">`+permalink+`</a>`,
		"The following notes were included:",
		"<b>Thought of you.</b>",
		"The body.",
		"The rest.",
	)
}
