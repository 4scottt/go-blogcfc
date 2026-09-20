package mail

// The blog's outgoing messages, one function per shape, each returning a
// Message the caller only has to hand to a Sender. The subjects and
// bodies are BlogCFC's (blog.cfc notifyEntry and mailEntry, the subscribe
// pod, admin/mailsubscribers.cfm, contact.cfm and send.cfm), carried over
// to text/template files embedded in the binary.
//
// Two deviations from the as-is, both deliberate:
//
//   - BlogCFC's comment and entry mails were HTML with inline CSS copied
//     from the Arclite skin. These are plain text (PLAN §11: templates,
//     subject and body from BlogCFC, not its styling). Only SendEntry
//     stays HTML, because it carries a rendered entry.
//   - The one HTML template escapes its scalar fields here, in Go, since
//     text/template does not escape; the pre-rendered entry body is
//     passed through untouched, as the as-is did.

import (
	"embed"
	"html"
	"net/url"
	"strings"
	"text/template"
)

//go:embed templates/*.txt
var templateFS embed.FS

var tmpl = template.Must(template.ParseFS(templateFS, "templates/*.txt"))

// exec renders one template; a template that cannot render is a
// programming error, not a runtime one, so it panics at start-up in the
// tests rather than silently mailing an empty body.
func exec(name string, data any) string {
	var b strings.Builder
	if err := tmpl.ExecuteTemplate(&b, name, data); err != nil {
		panic("mail: template " + name + ": " + err.Error())
	}
	return b.String()
}

// From returns the sender a comment notification uses: the `commentsfrom`
// setting when it is set, else `owneremail` (blog.cfc notifyEntry).
func From(commentsFrom, ownerEmail string) string {
	if s := strings.TrimSpace(commentsFrom); s != "" {
		return s
	}
	return strings.TrimSpace(ownerEmail)
}

// UnsubscribePlaceholder is the literal BlogCFC put in a notification
// body and replaced per recipient (notifyEntry). Personalize does that
// job here.
const UnsubscribePlaceholder = "%unsubscribe%"

// CommentNotificationVars is one new comment, as its notification shows
// it. Posted is already formatted in the blog's zone by the caller.
type CommentNotificationVars struct {
	BlogTitle   string
	EntryTitle  string
	EntryURL    string // the permalink, with the #c<id> anchor
	Author      string
	AuthorEmail string
	Website     string
	Comment     string
	Posted      string

	// From is the envelope sender: use From(commentsfrom, owneremail).
	From string
	// Subject overrides the English default when the caller has a bundle.
	Subject string
}

// CommentNotification is notifyEntry's message. Its body carries
// UnsubscribePlaceholder and no recipient: Personalize fills both in, per
// recipient, exactly as the as-is did (PLAN §11 "Notification
// recipients").
func CommentNotification(v CommentNotificationVars) Message {
	subject := v.Subject
	if subject == "" {
		subject = "Comment posted to " + v.BlogTitle + " : " + v.EntryTitle
	}
	return Message{
		From:    v.From,
		Subject: subject,
		Body:    exec("comment_notification.txt", v),
	}
}

// Recipient is one addressee of a comment notification.
type Recipient struct {
	Email string

	// CommentID is this recipient's own comment row, which their
	// unsubscribe link names (notifyEntry's email -> comment id map).
	CommentID string

	// Owner marks the blog owner's copy: Delete instead of Unsubscribe,
	// and Approve as well while moderation is on.
	Owner      bool
	KillToken  string
	ApproveID  string
	Moderating bool
}

// Personalize addresses a notification to one recipient and replaces
// UnsubscribePlaceholder with the links that recipient should see: a
// subscriber gets their per-comment unsubscribe link, the owner gets
// Delete and, when moderating, Approve (PLAN §9 C10, C11).
func Personalize(m Message, baseURL string, r Recipient) Message {
	base := strings.TrimRight(baseURL, "/")
	var block string
	switch {
	case r.Owner:
		block = "Delete this comment: " + base + "/?killcomment=" + url.QueryEscape(r.KillToken)
		if r.Moderating {
			block += "\nApprove this comment: " + base + "/?approvecomment=" + url.QueryEscape(r.ApproveID)
		}
	default:
		block = "Unsubscribe from Entry: " + base + "/unsubscribe?email=" +
			url.QueryEscape(r.Email) + "&commentID=" + url.QueryEscape(r.CommentID)
	}
	out := m
	out.To = []string{r.Email}
	out.Cc = nil
	out.Body = strings.ReplaceAll(m.Body, UnsubscribePlaceholder, block)
	return out
}

// NewEntryVars is one released entry as its subscribers' mail shows it.
// Body and MoreBody are already rendered by the caller; Email and Token
// are this subscriber's, and make the unsubscribe link.
type NewEntryVars struct {
	BaseURL   string
	BlogTitle string
	Title     string
	EntryURL  string
	Author    string
	Body      string
	HasMore   bool

	Email string
	Token string

	From    string
	Subject string
}

// NewEntry is mailEntry's message (PLAN §9 C15), one per subscriber.
func NewEntry(v NewEntryVars) Message {
	subject := v.Subject
	if subject == "" {
		subject = v.BlogTitle + " / " + v.Title
	}
	data := struct {
		NewEntryVars
		UnsubscribeURL string
	}{v, blogUnsubscribeURL(v.BaseURL, v.Email, v.Token)}
	return Message{
		To:      []string{v.Email},
		From:    v.From,
		Subject: subject,
		Body:    exec("new_entry.txt", data),
	}
}

// blogUnsubscribeURL is the blog-level unsubscribe link, by email and
// token (unsubscribe.cfm; PLAN §9 C14).
func blogUnsubscribeURL(baseURL, email, token string) string {
	return strings.TrimRight(baseURL, "/") + "/unsubscribe?email=" +
		url.QueryEscape(email) + "&token=" + url.QueryEscape(token)
}

// SubscribeConfirmationVars is the double opt-in mail (PLAN §9 C13).
// Subject and Intro override the English defaults when the caller has a
// localised bundle, which the pods package does.
type SubscribeConfirmationVars struct {
	BaseURL   string
	BlogTitle string
	To        string
	From      string
	Token     string

	Subject string
	Intro   string
}

// SubscribeConfirmation is the mail the subscribe pod sends.
func SubscribeConfirmation(v SubscribeConfirmationVars) Message {
	subject := v.Subject
	if subject == "" {
		subject = strings.TrimSpace(v.BlogTitle + " Subscription Confirmation")
	}
	intro := v.Intro
	if intro == "" {
		intro = "Please confirm your subscription to the blog by clicking the link below."
	}
	data := struct {
		SubscribeConfirmationVars
		Intro      string
		ConfirmURL string
	}{v, intro, strings.TrimRight(v.BaseURL, "/") + "/confirmsubscription?t=" + url.QueryEscape(v.Token)}
	return Message{
		To:      []string{v.To},
		From:    v.From,
		Subject: subject,
		Body:    exec("subscribe_confirmation.txt", data),
	}
}

// MailAllSubscribersVars is the admin's message to every verified
// subscriber (PLAN §9 A16, admin/mailsubscribers.cfm). Email and Token
// are this subscriber's: the unsubscribe block is appended per recipient,
// as the as-is page did in its own loop.
type MailAllSubscribersVars struct {
	BaseURL string
	Subject string
	Body    string
	From    string

	Email string
	Token string

	// HTML keeps the as-is page's promise that HTML is allowed in the
	// body; the appended block then uses tags rather than bare lines.
	HTML bool
}

// MailAllSubscribers builds one subscriber's copy of an admin broadcast.
func MailAllSubscribers(v MailAllSubscribersVars) Message {
	data := struct {
		MailAllSubscribersVars
		UnsubscribeURL string
	}{v, blogUnsubscribeURL(v.BaseURL, v.Email, v.Token)}
	return Message{
		To:      []string{v.Email},
		From:    v.From,
		Subject: v.Subject,
		Body:    strings.TrimRight(exec("mail_all_subscribers.txt", data), "\n") + "\n",
		HTML:    v.HTML,
	}
}

// ContactOwnerVars is contact.cfm's message to the blog owner (PLAN §9
// P21). The three labels come from the caller's bundle; When is already
// formatted in the blog's zone.
type ContactOwnerVars struct {
	To      string
	From    string
	Subject string

	Name     string
	Email    string
	IP       string
	When     string
	Comments string
	Version  string

	CommentAddedLabel  string
	CommentMadeByLabel string
	IPLabel            string
}

// ContactOwner is the contact form's mail.
func ContactOwner(v ContactOwnerVars) Message {
	if v.Subject == "" {
		v.Subject = "Contact Form"
	}
	v.CommentAddedLabel = orDefault(v.CommentAddedLabel, "Comment added")
	v.CommentMadeByLabel = orDefault(v.CommentMadeByLabel, "Comment made by")
	v.IPLabel = orDefault(v.IPLabel, "IP of Poster")
	return Message{
		To:      []string{v.To},
		From:    v.From,
		Subject: v.Subject,
		Body:    exec("contact_owner.txt", v),
	}
}

// SendEntryVars is send.cfm's email-this-entry (PLAN §9 P22). Body and
// MoreBody are rendered HTML and pass through untouched; every other
// field is escaped here, because the template is a text one.
type SendEntryVars struct {
	To      string
	Cc      string // the owner's copy, when `owneremail` is set
	From    string
	Subject string

	Sender    string // the visitor's own address
	BlogTitle string
	Title     string
	Link      string
	Notes     string
	Body      string
	MoreBody  string
}

// SendEntry is the email-this-entry mail, the one HTML message left.
func SendEntry(v SendEntryVars) Message {
	if v.Subject == "" {
		v.Subject = "Blog entry from: " + v.BlogTitle
	}
	data := v
	data.Sender = html.EscapeString(v.Sender)
	data.BlogTitle = html.EscapeString(v.BlogTitle)
	data.Title = html.EscapeString(v.Title)
	data.Link = html.EscapeString(v.Link)
	data.Notes = html.EscapeString(v.Notes)

	m := Message{
		To:      []string{v.To},
		From:    v.From,
		Subject: v.Subject,
		Body:    exec("send_entry.txt", data),
		HTML:    true,
	}
	if cc := strings.TrimSpace(v.Cc); cc != "" {
		m.Cc = []string{cc}
	}
	return m
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}
