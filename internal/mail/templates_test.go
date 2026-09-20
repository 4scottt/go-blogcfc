package mail

import (
	"strings"
	"testing"
)

const (
	base      = "http://blog.example"
	blogTitle = "Ray's Blog"
)

func commentVars() CommentNotificationVars {
	return CommentNotificationVars{
		BlogTitle:   blogTitle,
		EntryTitle:  "On Tags",
		EntryURL:    "http://blog.example/post/2026/9/19/on-tags#c42",
		Author:      "Ada",
		AuthorEmail: "ada@example.com",
		Website:     "http://ada.example",
		Comment:     "First!\n\nReally.",
		Posted:      "19 September 2026 12:00",
		From:        From("comments@blog.example", "owner@example.com"),
	}
}

// TestFP_C10_CommentNotificationTemplate covers the notification's
// subject, body and sender: `commentsfrom` wins over `owneremail`, and
// the body leaves %unsubscribe% for Personalize.
func TestFP_C10_CommentNotificationTemplate(t *testing.T) {
	m := CommentNotification(commentVars())

	if want := "Comment posted to Ray's Blog : On Tags"; m.Subject != want {
		t.Errorf("subject = %q, want %q", m.Subject, want)
	}
	if m.From != "comments@blog.example" {
		t.Errorf("From = %q, commentsfrom must win over owneremail", m.From)
	}
	if len(m.To) != 0 {
		t.Errorf("To = %v, the recipient is Personalize's job", m.To)
	}
	if m.HTML {
		t.Error("the notification is plain text")
	}

	want := `Comment Added to Ray's Blog : On Tags
http://blog.example/post/2026/9/19/on-tags#c42

First!

Really.

------------------------------------------------------------
Comment made by: Ada (ada@example.com)
Website: http://ada.example
Posted: 19 September 2026 12:00

%unsubscribe%
`
	if m.Body != want {
		t.Errorf("body =\n%q\nwant\n%q", m.Body, want)
	}

	t.Run("owneremail when commentsfrom is unset", func(t *testing.T) {
		if got := From("", "owner@example.com"); got != "owner@example.com" {
			t.Errorf("From = %q", got)
		}
		if got := From("   ", "owner@example.com"); got != "owner@example.com" {
			t.Errorf("a blank commentsfrom is unset: %q", got)
		}
	})

	t.Run("no website and no posted time", func(t *testing.T) {
		v := commentVars()
		v.Website = ""
		v.Posted = ""
		body := CommentNotification(v).Body
		if strings.Contains(body, "Website:") || strings.Contains(body, "Posted:") {
			t.Errorf("empty fields must leave no empty label:\n%s", body)
		}
	})
}

// TestFP_C10_PerRecipientUnsubscribeSubstitution covers the one message
// built once and addressed many times (notifyEntry): a subscriber's copy
// carries their own unsubscribe link, the owner's carries Delete and, in
// moderation, Approve.
func TestFP_C10_PerRecipientUnsubscribeSubstitution(t *testing.T) {
	m := CommentNotification(commentVars())

	sub := Personalize(m, base+"/", Recipient{Email: "bob@example.com", CommentID: "c7"})
	if len(sub.To) != 1 || sub.To[0] != "bob@example.com" {
		t.Fatalf("To = %v", sub.To)
	}
	wantLink := "Unsubscribe from Entry: http://blog.example/unsubscribe?email=bob%40example.com&commentID=c7"
	if !strings.HasSuffix(sub.Body, wantLink+"\n") {
		t.Errorf("subscriber body ends\n%q\nwant it to end with\n%q", tail(sub.Body), wantLink)
	}
	if strings.Contains(sub.Body, UnsubscribePlaceholder) {
		t.Error("the placeholder survived")
	}

	owner := Personalize(m, base, Recipient{
		Email: "owner@example.com", Owner: true, KillToken: "kill-42", ApproveID: "c42", Moderating: true,
	})
	wantOwner := "Delete this comment: http://blog.example/?killcomment=kill-42\n" +
		"Approve this comment: http://blog.example/?approvecomment=c42"
	if !strings.HasSuffix(owner.Body, wantOwner+"\n") {
		t.Errorf("owner body ends\n%q\nwant it to end with\n%q", tail(owner.Body), wantOwner)
	}
	if strings.Contains(owner.Body, "Unsubscribe") {
		t.Error("the owner is not a subscriber and gets no unsubscribe link")
	}

	noMod := Personalize(m, base, Recipient{Email: "owner@example.com", Owner: true, KillToken: "kill-42"})
	if strings.Contains(noMod.Body, "approvecomment") {
		t.Error("Approve belongs only in a moderated blog's mail")
	}

	// Personalize must not mutate the message it was given.
	if !strings.Contains(m.Body, UnsubscribePlaceholder) {
		t.Error("Personalize modified the original message")
	}
}

// TestFP_C15_NewEntryTemplate covers mailEntry: title, url, author, body,
// the continued link and the blog-level unsubscribe block.
func TestFP_C15_NewEntryTemplate(t *testing.T) {
	v := NewEntryVars{
		BaseURL: base, BlogTitle: blogTitle, Title: "On Tags",
		EntryURL: "http://blog.example/post/on-tags", Author: "ray",
		Body: "The body.", HasMore: true,
		Email: "bob@example.com", Token: "tok en", From: "owner@example.com",
	}
	m := NewEntry(v)

	if want := "Ray's Blog / On Tags"; m.Subject != want {
		t.Errorf("subject = %q, want %q", m.Subject, want)
	}
	if len(m.To) != 1 || m.To[0] != "bob@example.com" {
		t.Fatalf("To = %v", m.To)
	}
	want := `On Tags

URL: http://blog.example/post/on-tags
Author: ray

The body.

[Continued at Blog] http://blog.example/post/on-tags

You are receiving this email because you have subscribed to this blog.
To unsubscribe, please go to this URL:
http://blog.example/unsubscribe?email=bob%40example.com&token=tok+en
`
	if m.Body != want {
		t.Errorf("body =\n%q\nwant\n%q", m.Body, want)
	}

	v.HasMore = false
	if body := NewEntry(v).Body; strings.Contains(body, "Continued") {
		t.Errorf("an entry with no morebody has no continued link:\n%s", body)
	}
}

// TestSubscribeConfirmationTemplate covers the double opt-in mail the
// subscribe pod sends today inline (PLAN §9 C13).
func TestSubscribeConfirmationTemplate(t *testing.T) {
	m := SubscribeConfirmation(SubscribeConfirmationVars{
		BaseURL: base, BlogTitle: blogTitle, To: "bob@example.com",
		From: "owner@example.com", Token: "t/1",
	})
	if want := "Ray's Blog Subscription Confirmation"; m.Subject != want {
		t.Errorf("subject = %q, want %q", m.Subject, want)
	}
	want := `Please confirm your subscription to the blog by clicking the link below.

http://blog.example/confirmsubscription?t=t%2F1
`
	if m.Body != want {
		t.Errorf("body =\n%q\nwant\n%q", m.Body, want)
	}

	// A caller with a bundle overrides both strings.
	l := SubscribeConfirmation(SubscribeConfirmationVars{
		BaseURL: base, To: "bob@example.com", Token: "t1",
		Subject: "Abonnement bestätigen", Intro: "Bitte bestätigen Sie.",
	})
	if l.Subject != "Abonnement bestätigen" || !strings.HasPrefix(l.Body, "Bitte bestätigen Sie.") {
		t.Errorf("localised strings ignored: %q / %q", l.Subject, l.Body)
	}
}

// TestFP_A16_MailAllSubscribersTemplate covers the admin broadcast: the
// operator's body, then a per-recipient unsubscribe block, in text and in
// the HTML the as-is page allowed.
func TestFP_A16_MailAllSubscribersTemplate(t *testing.T) {
	v := MailAllSubscribersVars{
		BaseURL: base, Subject: "News", Body: "Hello all.",
		From: "owner@example.com", Email: "bob@example.com", Token: "t1",
	}
	m := MailAllSubscribers(v)
	if m.Subject != "News" || len(m.To) != 1 || m.To[0] != "bob@example.com" || m.HTML {
		t.Fatalf("envelope = %+v", m)
	}
	want := `Hello all.

You are receiving this email because you have subscribed to this blog.
To unsubscribe, please go to this URL:
http://blog.example/unsubscribe?email=bob%40example.com&token=t1
`
	if m.Body != want {
		t.Errorf("body =\n%q\nwant\n%q", m.Body, want)
	}

	v.Body = "<b>Hello</b>"
	v.HTML = true
	h := MailAllSubscribers(v)
	if !h.HTML {
		t.Error("HTML flag lost")
	}
	wantHTML := `<b>Hello</b><br />
<p>
You are receiving this email because you have subscribed to this blog.<br />
To unsubscribe, please go to this URL:
<a href="http://blog.example/unsubscribe?email=bob%40example.com&token=t1">http://blog.example/unsubscribe?email=bob%40example.com&token=t1</a>
</p>
`
	if h.Body != wantHTML {
		t.Errorf("html body =\n%q\nwant\n%q", h.Body, wantHTML)
	}

	// Two subscribers get two different links from the same body.
	v.HTML = false
	v.Body = "Hello all."
	v.Email, v.Token = "carol@example.com", "t2"
	if other := MailAllSubscribers(v); other.Body == m.Body {
		t.Error("every subscriber must get their own unsubscribe link")
	}
}

// TestContactOwnerTemplate covers contact.cfm's mail, which the web
// handler inlines today; the labels come from the caller's bundle.
func TestContactOwnerTemplate(t *testing.T) {
	m := ContactOwner(ContactOwnerVars{
		To: "owner@example.com", From: "owner@example.com",
		Name: "Ada", Email: "ada@example.com", IP: "203.0.113.9",
		When: "19 September 2026 / 12:00", Comments: "Hi.", Version: "0.1.0",
	})
	if m.Subject != "Contact Form" {
		t.Errorf("subject = %q", m.Subject)
	}
	want := `Comment added: 19 September 2026 / 12:00
Comment made by: Ada (ada@example.com)
IP of Poster: 203.0.113.9

Hi.

------------------------------------------------------------
This blog powered by go-blogcfc 0.1.0, a rewrite of BlogCFC by Raymond Camden
`
	if m.Body != want {
		t.Errorf("body =\n%q\nwant\n%q", m.Body, want)
	}

	de := ContactOwner(ContactOwnerVars{
		To: "owner@example.com", Subject: "Kontaktformular",
		CommentAddedLabel: "Kommentar hinzugefügt", CommentMadeByLabel: "Kommentar von", IPLabel: "IP",
	})
	if de.Subject != "Kontaktformular" || !strings.HasPrefix(de.Body, "Kommentar hinzugefügt: ") {
		t.Errorf("bundle labels ignored: %q / %q", de.Subject, de.Body)
	}
}

// TestSendEntryTemplate covers send.cfm's email-this-entry, the one
// message that stays HTML: the rendered entry passes through, everything
// the visitor typed is escaped.
func TestSendEntryTemplate(t *testing.T) {
	m := SendEntry(SendEntryVars{
		To: "bob@example.com", Cc: "owner@example.com", From: "owner@example.com",
		Sender: "ada@example.com", BlogTitle: blogTitle, Title: "On <Tags>",
		Link: "http://blog.example/post/on-tags", Notes: `<script>alert(1)</script>`,
		Body: "<p>The body.</p>", MoreBody: "<p>More.</p>",
	})
	if want := "Blog entry from: Ray's Blog"; m.Subject != want {
		t.Errorf("subject = %q, want %q", m.Subject, want)
	}
	if !m.HTML {
		t.Error("the entry mail is HTML")
	}
	if len(m.Cc) != 1 || m.Cc[0] != "owner@example.com" {
		t.Errorf("Cc = %v, the owner gets a copy when owneremail is set", m.Cc)
	}
	if strings.Contains(m.Body, "<script>") {
		t.Errorf("the visitor's notes were not escaped:\n%s", m.Body)
	}
	if !strings.Contains(m.Body, "On &lt;Tags&gt;") {
		t.Errorf("the title was not escaped:\n%s", m.Body)
	}
	if !strings.HasSuffix(m.Body, "<p>The body.</p><p>More.</p>\n") {
		t.Errorf("the rendered entry must pass through:\n%s", m.Body)
	}

	if none := SendEntry(SendEntryVars{To: "bob@example.com", BlogTitle: blogTitle}); len(none.Cc) != 0 {
		t.Errorf("Cc = %v with no owneremail", none.Cc)
	}
}

func tail(s string) string {
	if len(s) > 120 {
		return "..." + s[len(s)-120:]
	}
	return s
}
