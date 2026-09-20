package admin_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/4scottt/go-blogcfc/internal/store"
)

// subscribe signs an address up, verifying it when asked, and returns the
// row with its token.
func (h *harness) subscribe(email string, verified bool) store.Subscriber {
	h.t.Helper()
	ctx := context.Background()
	sub, _, err := h.store.AddSubscriber(ctx, email)
	if err != nil {
		h.t.Fatalf("AddSubscriber(%s): %v", email, err)
	}
	if verified {
		if err := h.store.VerifySubscriber(ctx, email); err != nil {
			h.t.Fatalf("VerifySubscriber(%s): %v", email, err)
		}
		sub.Verified = true
	}
	return sub
}

// TestFP_A15_SubscribersListDeleteVerifyRemoveUnverified covers PLAN §9
// A15: the list with its two totals, verifying by hand, deleting one
// address and removing every unverified one (admin/subscribers.cfm).
func TestFP_A15_SubscribersListDeleteVerifyRemoveUnverified(t *testing.T) {
	h := newHarness(t)
	h.user("admin", "Admin")

	h.subscribe("ada@example.com", true)
	h.subscribe("bob@example.com", false)
	h.subscribe("cyd@example.com", false)
	h.subscribe("dee@example.com", false)

	h.login("admin")
	resp, body := h.get("/admin/subscribers")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("subscribers: status %d, want 200", resp.StatusCode)
	}
	for _, want := range []string{
		"<h1>Subscribers</h1>",
		"4 subscribers, 1 verified.",
		"ada@example.com", "bob@example.com",
		`name="verify"`, `value="Verify"`,
		`name="delete"`, `value="Delete"`,
		`name="nukeunverified"`, `value="Remove Unverified"`,
		`action="/admin/subscribers"`,
		"/admin/subscribers/mail",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the subscribers list is missing %s", want)
		}
	}

	// Verify by hand.
	resp, _ = h.postForm("/admin/subscribers", url.Values{"email": {"bob@example.com"}, "verify": {"Verify"}})
	loc := redirectedTo(t, resp, "/admin/subscribers?verified=1")
	if sub, err := h.store.GetSubscriber(context.Background(), "bob@example.com"); err != nil || !sub.Verified {
		t.Errorf("bob is %+v (err %v), want verified", sub, err)
	}
	_, body = h.get(loc)
	if !strings.Contains(body, "Subscriber verified.") {
		t.Error("the list does not report the verification")
	}

	// Delete one address.
	resp, _ = h.postForm("/admin/subscribers", url.Values{"email": {"cyd@example.com"}, "delete": {"Delete"}})
	redirectedTo(t, resp, "/admin/subscribers?deleted=1")
	if _, err := h.store.GetSubscriber(context.Background(), "cyd@example.com"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("cyd is still subscribed (err %v)", err)
	}

	// Remove every unverified address: only dee is left unverified.
	resp, _ = h.postForm("/admin/subscribers", url.Values{"nukeunverified": {"Remove Unverified"}})
	loc = redirectedTo(t, resp, "/admin/subscribers?removed=1")
	_, body = h.get(loc)
	if !strings.Contains(body, "1 unverified subscriber removed.") {
		t.Error("the list does not report the mass delete")
	}
	if !strings.Contains(body, "2 subscribers, 2 verified.") {
		t.Error("the totals did not follow the changes")
	}
	if strings.Contains(body, "dee@example.com") {
		t.Error("an unverified subscriber survived Remove Unverified")
	}
}
