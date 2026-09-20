package pods

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// TestFP_D06_SearchPod: the search box (PLAN §9 D06, pods/search.cfm).
func TestFP_D06_SearchPod(t *testing.T) {
	s := newTestSite(t)
	s.only(Search)

	got := s.sidebar("/")
	checkGolden(t, "search.html", got)
	mustContain(t, got,
		`<h4>Search</h4>`,
		`<form action="`+testBase+`/search" method="get"`,
		`<input type="text" name="search" size="15" />`,
	)
}

// TestFP_D07_SubscribePod: the form, the confirmation mail with its
// token link, and the address that was already on the list (PLAN §9 D07,
// C13, pods/subscribe.cfm).
func TestFP_D07_SubscribePod(t *testing.T) {
	s := newTestSite(t)
	s.only(Subscribe)

	pod := s.sidebar("/")
	checkGolden(t, "subscribe.html", pod)
	mustContain(t, pod,
		`<h4>Subscribe</h4>`,
		`Enter your email address to subscribe to this blog.`,
		`<form action="`+testBase+`/subscribe" method="post"`,
		`<input type="text" name="email" size="15" />`,
	)

	// A new address: one mail, carrying the confirmation link.
	rec := s.post("/subscribe", map[string]string{"email": "reader%40example.com"})
	if rec.Code != http.StatusOK {
		t.Fatalf("subscribe: status %d", rec.Code)
	}
	mustContain(t, rec.Body.String(), "Please confirm your subscription")
	msgs := s.mails.Messages()
	if len(msgs) != 1 {
		t.Fatalf("subscribing sent %d messages, want 1", len(msgs))
	}
	sub, err := s.store.GetSubscriber(context.Background(), "reader@example.com")
	if err != nil {
		t.Fatalf("subscriber not stored: %v", err)
	}
	if sub.Verified {
		t.Error("a new subscriber is verified before confirming")
	}
	m := msgs[0]
	if len(m.To) != 1 || m.To[0] != "reader@example.com" {
		t.Errorf("mail went to %v", m.To)
	}
	if m.From != "owner@example.com" {
		t.Errorf("mail came from %q, want the owner", m.From)
	}
	if want := "Test Blog Subscription Confirmation"; m.Subject != want {
		t.Errorf("subject %q, want %q", m.Subject, want)
	}
	if link := testBase + "/confirmsubscription?t=" + sub.Token; !strings.Contains(m.Body, link) {
		t.Errorf("mail body has no confirmation link %q:\n%s", link, m.Body)
	}

	// The same address again: no second mail, and the pod says so.
	s.mails.Reset()
	again := s.post("/subscribe", map[string]string{"email": "reader%40example.com"})
	mustContain(t, again.Body.String(), "already subscribed")
	if n := len(s.mails.Messages()); n != 0 {
		t.Errorf("an address already on the list sent %d messages, want 0", n)
	}

	// Not an address at all: BlogCFC's isEmail refuses it.
	bad := s.post("/subscribe", map[string]string{"email": "not-an-address"})
	mustContain(t, bad.Body.String(), "You must include a valid email address.")
	if n := len(s.mails.Messages()); n != 0 {
		t.Errorf("an invalid address sent %d messages, want 0", n)
	}
}

// TestFP_D07_SubscribeEmailSyntax is isEmail, the as-is validator
// (org/camden/blog/utils.cfc).
func TestFP_D07_SubscribeEmailSyntax(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"reader@example.com", true},
		{"reader+tag@example.com", true},
		{"first.last@mail.example.co.uk", true},
		{"reader@example.museum", true},
		{"", false},
		{"reader", false},
		{"reader@example", false},
		{"reader@@example.com", false},
		{"reader@example.c", false},
		{"<script>@example.com", false},
		{strings.Repeat("a", 65) + "@example.com", false},
	}
	for _, tc := range cases {
		if got := validEmail(tc.in); got != tc.want {
			t.Errorf("validEmail(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
