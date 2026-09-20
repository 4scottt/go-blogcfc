package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/4scottt/go-blogcfc/internal/store"
)

// subscriber puts one address on the blog's list, confirmed or not.
func (s *testSite) subscriber(t *testing.T, email string, verified bool) store.Subscriber {
	t.Helper()
	sub, _, err := s.store.AddSubscriber(context.Background(), email)
	if err != nil {
		t.Fatalf("add subscriber %q: %v", email, err)
	}
	if verified {
		if _, err := s.store.ConfirmSubscriber(context.Background(), sub.Token); err != nil {
			t.Fatalf("confirm subscriber %q: %v", email, err)
		}
		sub.Verified = true
	}
	return sub
}

// verified reads an address's confirmation flag back.
func (s *testSite) verified(t *testing.T, email string) bool {
	t.Helper()
	sub, err := s.store.GetSubscriber(context.Background(), email)
	if err != nil {
		t.Fatalf("get subscriber %q: %v", email, err)
	}
	return sub.Verified
}

// subscribed reads a comment's subscribe flag back.
func (s *testSite) subscribed(t *testing.T, commentID string) bool {
	t.Helper()
	c, err := s.store.GetComment(context.Background(), commentID)
	if err != nil {
		t.Fatalf("get comment %s: %v", commentID, err)
	}
	return c.Subscribe
}

// TestFP_C13_ConfirmSubscriptionByTokenAndUnknown is the double opt-in's
// second half: the link in the confirmation mail verifies the address,
// an unknown token verifies nobody, and no token at all goes home
// (PLAN §9 C13, client/confirmsubscription.cfm).
func TestFP_C13_ConfirmSubscriptionByTokenAndUnknown(t *testing.T) {
	s := newTestSite(t, nil)
	sub := s.subscriber(t, "reader@example.com", false)
	other := s.subscriber(t, "another@example.com", false)

	if s.verified(t, sub.Email) {
		t.Fatal("a new subscriber is verified before confirming")
	}

	body := s.getOK("/confirmsubscription?t=" + url.QueryEscape(sub.Token))
	for _, want := range []string{
		"Subscription Confirmation",
		"Thank you for confirming your subscription to this blog.",
		`<a href="` + testBase + `/">Return to the Blog</a>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the confirmation page is missing %q", want)
		}
	}
	checkGolden(t, "subscription_confirmed.html", postBlock.FindString(body))
	if !s.verified(t, sub.Email) {
		t.Error("the token did not verify its subscriber")
	}
	if s.verified(t, other.Email) {
		t.Error("confirming one address verified another")
	}

	// An unknown token: the page says so and nobody is verified.
	unknown := s.getOK("/confirmsubscription?t=not-a-token")
	if !strings.Contains(unknown, "was not confirmed") {
		t.Errorf("an unknown token did not say so:\n%s", postBlock.FindString(unknown))
	}
	if s.verified(t, other.Email) {
		t.Error("an unknown token verified an address")
	}

	// No token at all: home, as the as-is cflocation did.
	rec := s.get("/confirmsubscription")
	if rec.Code != http.StatusFound {
		t.Errorf("GET /confirmsubscription without a token = %d, want %d", rec.Code, http.StatusFound)
	}
	if got := rec.Header().Get("Location"); got != testBase+"/" {
		t.Errorf("redirected to %q, want the blog's home", got)
	}
}

// TestFP_C14_UnsubscribeThreadAndBlogForms is unsubscribe.cfm's two
// links: `?email=&commentID=` takes an address off a comment thread and
// `?email=&token=` off the blog. A wrong id, a wrong address or a wrong
// token changes nothing and says so (PLAN §9 C14).
func TestFP_C14_UnsubscribeThreadAndBlogForms(t *testing.T) {
	s := newTestSite(t, nil)
	ctx := context.Background()
	e := s.entry(store.Entry{Title: "Thread", Alias: "thread", Posted: utc(2011, 3, 4, 9, 0),
		Username: "ray", Released: true, AllowComments: true})
	other := s.entry(store.Entry{Title: "Elsewhere", Alias: "elsewhere", Posted: utc(2011, 3, 5, 9, 0),
		Username: "ray", Released: true, AllowComments: true})

	first := s.comment(store.Comment{EntryID: e.ID, Name: "Reader", Email: "reader@example.com",
		Comment: "One", Posted: utc(2011, 3, 4, 10, 0), Subscribe: true, Moderated: true})
	second := s.comment(store.Comment{EntryID: e.ID, Name: "Reader", Email: "reader@example.com",
		Comment: "Two", Posted: utc(2011, 3, 4, 11, 0), Subscribe: true, Moderated: true})
	someoneElse := s.comment(store.Comment{EntryID: e.ID, Name: "Other", Email: "other@example.com",
		Comment: "Three", Posted: utc(2011, 3, 4, 12, 0), Subscribe: true, Moderated: true})
	elsewhere := s.comment(store.Comment{EntryID: other.ID, Name: "Reader", Email: "reader@example.com",
		Comment: "Four", Posted: utc(2011, 3, 5, 10, 0), Subscribe: true, Moderated: true})

	// The thread form, with a comment id and the address that left it.
	body := s.getOK("/unsubscribe?email=" + url.QueryEscape("reader@example.com") +
		"&commentID=" + url.QueryEscape(first.ID))
	for _, want := range []string{"Unsubscribe", "You have been unsubscribed from the thread.",
		`<a href="` + testBase + `/">Return to the Blog</a>`} {
		if !strings.Contains(body, want) {
			t.Errorf("the thread unsubscribe page is missing %q", want)
		}
	}
	checkGolden(t, "subscription_unsubscribed_thread.html", postBlock.FindString(body))

	// Every comment that address left on the entry is off, and only that
	// address on only that entry (blog.cfc unsubscribeThread).
	if s.subscribed(t, first.ID) || s.subscribed(t, second.ID) {
		t.Error("the address is still subscribed to the thread")
	}
	if !s.subscribed(t, someoneElse.ID) {
		t.Error("unsubscribing one address took another off the thread")
	}
	if !s.subscribed(t, elsewhere.ID) {
		t.Error("unsubscribing from one entry took the address off another")
	}

	// A comment id with the wrong address: nothing changes.
	wrong := s.getOK("/unsubscribe?email=" + url.QueryEscape("nobody@example.com") +
		"&commentID=" + url.QueryEscape(someoneElse.ID))
	if !strings.Contains(wrong, "You have not been unsubscribed from the thread.") {
		t.Errorf("a mismatched pair did not say so:\n%s", postBlock.FindString(wrong))
	}
	if !s.subscribed(t, someoneElse.ID) {
		t.Error("a mismatched pair unsubscribed a comment anyway")
	}
	// An id that is no comment at all: the same answer.
	missing := s.getOK("/unsubscribe?email=" + url.QueryEscape("reader@example.com") +
		"&commentID=00000000-0000-4000-8000-000000000000")
	if !strings.Contains(missing, "You have not been unsubscribed from the thread.") {
		t.Errorf("an unknown comment id did not say so:\n%s", postBlock.FindString(missing))
	}

	// The blog form: the address and its token.
	sub := s.subscriber(t, "subscriber@example.com", true)
	keep := s.subscriber(t, "keeper@example.com", true)

	badToken := s.getOK("/unsubscribe?email=" + url.QueryEscape(sub.Email) + "&token=not-the-token")
	if !strings.Contains(badToken, "You have not been unsubscribed from the blog.") {
		t.Errorf("a wrong token did not say so:\n%s", postBlock.FindString(badToken))
	}
	if _, err := s.store.GetSubscriber(ctx, sub.Email); err != nil {
		t.Errorf("a wrong token removed the subscriber anyway: %v", err)
	}
	// Somebody else's token is a wrong token.
	othersToken := s.getOK("/unsubscribe?email=" + url.QueryEscape(sub.Email) +
		"&token=" + url.QueryEscape(keep.Token))
	if !strings.Contains(othersToken, "You have not been unsubscribed from the blog.") {
		t.Errorf("another subscriber's token did not say so:\n%s", postBlock.FindString(othersToken))
	}

	ok := s.getOK("/unsubscribe?email=" + url.QueryEscape(sub.Email) + "&token=" + url.QueryEscape(sub.Token))
	if !strings.Contains(ok, "You have been unsubscribed from the blog.") {
		t.Errorf("the blog unsubscribe page is missing its message:\n%s", postBlock.FindString(ok))
	}
	checkGolden(t, "subscription_unsubscribed_blog.html", postBlock.FindString(ok))
	if _, err := s.store.GetSubscriber(ctx, sub.Email); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetSubscriber after unsubscribing = %v, want ErrNotFound", err)
	}
	if _, err := s.store.GetSubscriber(ctx, keep.Email); err != nil {
		t.Errorf("unsubscribing one address removed another: %v", err)
	}

	// No address at all: home, as the as-is cflocation did.
	rec := s.get("/unsubscribe")
	if rec.Code != http.StatusFound {
		t.Errorf("GET /unsubscribe without an address = %d, want %d", rec.Code, http.StatusFound)
	}
	if got := rec.Header().Get("Location"); got != testBase+"/" {
		t.Errorf("redirected to %q, want the blog's home", got)
	}
}
