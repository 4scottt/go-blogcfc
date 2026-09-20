package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/4scottt/go-blogcfc/internal/store"
	"github.com/4scottt/go-blogcfc/internal/testdb"
)

// TestSubscriberDoubleOptInAndRemoval walks the blog subscription: sign
// up, confirm by token, count, verify by hand, unsubscribe by token and
// the sweep of everyone who never confirmed.
func TestSubscriberDoubleOptInAndRemoval(t *testing.T) {
	st := testdb.New(t)
	ctx := context.Background()

	sub, existed, err := st.AddSubscriber(ctx, " reader@example.com ")
	if err != nil {
		t.Fatalf("AddSubscriber: %v", err)
	}
	if existed {
		t.Error("the first sign-up reports the address as already there")
	}
	if sub.Email != "reader@example.com" || sub.Token == "" || sub.Verified {
		t.Fatalf("new subscriber = %+v, want a trimmed address, a token and no verification", sub)
	}

	again, existed, err := st.AddSubscriber(ctx, "reader@example.com")
	if err != nil {
		t.Fatalf("AddSubscriber: %v", err)
	}
	if !existed || again.Token != sub.Token {
		t.Fatalf("the second sign-up = %+v (existed %v), want the same row back", again, existed)
	}
	if _, _, err := st.AddSubscriber(ctx, "  "); err == nil {
		t.Error("an empty address was accepted")
	}

	ok, err := st.ConfirmSubscriber(ctx, sub.Token)
	if err != nil || !ok {
		t.Fatalf("ConfirmSubscriber = %v (%v), want true", ok, err)
	}
	if ok, err := st.ConfirmSubscriber(ctx, "not-a-token"); err != nil || ok {
		t.Fatalf("an unknown token = %v (%v), want false and no error", ok, err)
	}
	if ok, err := st.ConfirmSubscriber(ctx, ""); err != nil || ok {
		t.Fatalf("an empty token = %v (%v), want false and no error", ok, err)
	}
	// Confirming twice is idempotent, not an error.
	if ok, err := st.ConfirmSubscriber(ctx, sub.Token); err != nil || !ok {
		t.Fatalf("confirming twice = %v (%v)", ok, err)
	}

	got, err := st.GetSubscriber(ctx, "reader@example.com")
	if err != nil || !got.Verified {
		t.Fatalf("GetSubscriber = %+v (%v), want verified", got, err)
	}
	if _, err := st.GetSubscriber(ctx, "nobody@example.com"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("an unknown address gave %v, want ErrNotFound", err)
	}

	pending, _, err := st.AddSubscriber(ctx, "pending@example.com")
	if err != nil {
		t.Fatalf("AddSubscriber: %v", err)
	}
	byHand, _, err := st.AddSubscriber(ctx, "byhand@example.com")
	if err != nil {
		t.Fatalf("AddSubscriber: %v", err)
	}
	if err := st.VerifySubscriber(ctx, byHand.Email); err != nil {
		t.Fatalf("VerifySubscriber: %v", err)
	}
	if err := st.VerifySubscriber(ctx, "nobody@example.com"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("verifying an unknown address gave %v, want ErrNotFound", err)
	}

	list, err := st.ListSubscribers(ctx)
	if err != nil {
		t.Fatalf("ListSubscribers: %v", err)
	}
	if len(list) != 3 || list[0].Email != "byhand@example.com" || list[2].Email != "reader@example.com" {
		t.Fatalf("subscribers = %+v, want three, by address", list)
	}
	verified, err := st.VerifiedSubscribers(ctx)
	if err != nil || len(verified) != 2 {
		t.Fatalf("verified = %+v (%v), want two", verified, err)
	}
	total, verifiedCount, err := st.CountSubscribers(ctx)
	if err != nil || total != 3 || verifiedCount != 2 {
		t.Fatalf("CountSubscribers = %d/%d (%v), want 3/2", verifiedCount, total, err)
	}

	// The unsubscribe link needs both halves to match.
	if gone, err := st.RemoveSubscriberByToken(ctx, "reader@example.com", "wrong-token"); err != nil || gone {
		t.Fatalf("a wrong token removed the row: %v (%v)", gone, err)
	}
	if gone, err := st.RemoveSubscriberByToken(ctx, "reader@example.com", ""); err != nil || gone {
		t.Fatalf("an empty token removed the row: %v (%v)", gone, err)
	}
	gone, err := st.RemoveSubscriberByToken(ctx, "reader@example.com", sub.Token)
	if err != nil || !gone {
		t.Fatalf("RemoveSubscriberByToken = %v (%v), want true", gone, err)
	}
	if _, err := st.GetSubscriber(ctx, "reader@example.com"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("the unsubscribed address gave %v, want ErrNotFound", err)
	}

	n, err := st.DeleteUnverified(ctx)
	if err != nil || n != 1 {
		t.Fatalf("DeleteUnverified removed %d (%v), want 1", n, err)
	}
	if _, err := st.GetSubscriber(ctx, pending.Email); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("the unconfirmed address survived: %v", err)
	}
	if err := st.DeleteSubscriber(ctx, byHand.Email); err != nil {
		t.Fatalf("DeleteSubscriber: %v", err)
	}
	total, verifiedCount, err = st.CountSubscribers(ctx)
	if err != nil || total != 0 || verifiedCount != 0 {
		t.Fatalf("CountSubscribers = %d/%d (%v), want an empty list", verifiedCount, total, err)
	}
}
