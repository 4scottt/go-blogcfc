package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Subscriber is someone signed up for new-entry mail. Verified is the
// double opt-in flag; Token is the secret in the confirmation and
// unsubscribe links (PLAN §9 C13, C14).
type Subscriber struct {
	Email    string
	Token    string
	Verified bool
}

// AddSubscriber signs an address up, unverified, with a fresh token. It
// reports whether the address was already on the list, which is BlogCFC's
// "you are already subscribed" path (addSubscriber returns no token then):
// an existing row keeps its token and its verified flag.
func (s *Store) AddSubscriber(ctx context.Context, email string) (Subscriber, bool, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return Subscriber{}, false, fmt.Errorf("store: add subscriber: empty email")
	}
	if existing, err := s.GetSubscriber(ctx, email); err == nil {
		return *existing, true, nil
	} else if !errors.Is(err, ErrNotFound) {
		return Subscriber{}, false, err
	}

	token, err := newID()
	if err != nil {
		return Subscriber{}, false, err
	}
	sub := Subscriber{Email: email, Token: token}
	_, err = s.db.ExecContext(ctx,
		"INSERT INTO subscribers (email, token, verified) VALUES (?,?,0)", sub.Email, sub.Token)
	if err != nil {
		if isDuplicate(err) {
			// Two sign-ups raced; the row that won is the answer.
			existing, getErr := s.GetSubscriber(ctx, email)
			if getErr != nil {
				return Subscriber{}, false, getErr
			}
			return *existing, true, nil
		}
		return Subscriber{}, false, fmt.Errorf("store: add subscriber: %w", err)
	}
	return sub, false, nil
}

// ConfirmSubscriber verifies the address a confirmation token names. It
// reports whether the token matched; an unknown token is not an error, it
// is the "bad link" page.
func (s *Store) ConfirmSubscriber(ctx context.Context, token string) (bool, error) {
	if strings.TrimSpace(token) == "" {
		return false, nil
	}
	var email string
	err := s.db.QueryRowContext(ctx, "SELECT email FROM subscribers WHERE token = ? LIMIT 1", token).Scan(&email)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("store: confirm subscriber: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE subscribers SET verified = 1 WHERE email = ?", email); err != nil {
		return false, fmt.Errorf("store: confirm subscriber: %w", err)
	}
	return true, nil
}

// GetSubscriber returns one subscriber by address.
func (s *Store) GetSubscriber(ctx context.Context, email string) (*Subscriber, error) {
	var sub Subscriber
	err := s.db.QueryRowContext(ctx,
		"SELECT email, token, verified FROM subscribers WHERE email = ? LIMIT 1", strings.TrimSpace(email)).
		Scan(&sub.Email, &sub.Token, &sub.Verified)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get subscriber: %w", err)
	}
	return &sub, nil
}

// ListSubscribers returns everyone on the list, by address.
func (s *Store) ListSubscribers(ctx context.Context) ([]Subscriber, error) {
	return s.subscribers(ctx, false)
}

// VerifiedSubscribers returns only the confirmed addresses: the ones a
// new entry's mail goes to (PLAN §9 C15, A16).
func (s *Store) VerifiedSubscribers(ctx context.Context) ([]Subscriber, error) {
	return s.subscribers(ctx, true)
}

func (s *Store) subscribers(ctx context.Context, verifiedOnly bool) ([]Subscriber, error) {
	q := "SELECT email, token, verified FROM subscribers"
	if verifiedOnly {
		q += " WHERE verified = 1"
	}
	q += " ORDER BY email ASC"
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("store: list subscribers: %w", err)
	}
	defer rows.Close()
	var out []Subscriber
	for rows.Next() {
		var sub Subscriber
		if err := rows.Scan(&sub.Email, &sub.Token, &sub.Verified); err != nil {
			return nil, fmt.Errorf("store: list subscribers: %w", err)
		}
		out = append(out, sub)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list subscribers: %w", err)
	}
	return out, nil
}

// CountSubscribers is the pair of numbers the admin screen shows.
func (s *Store) CountSubscribers(ctx context.Context) (total, verified int, err error) {
	err = s.db.QueryRowContext(ctx,
		"SELECT COUNT(*), COALESCE(SUM(verified = 1), 0) FROM subscribers").Scan(&total, &verified)
	if err != nil {
		return 0, 0, fmt.Errorf("store: count subscribers: %w", err)
	}
	return total, verified, nil
}

// VerifySubscriber confirms an address by hand, from the admin screen.
func (s *Store) VerifySubscriber(ctx context.Context, email string) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE subscribers SET verified = 1 WHERE email = ?", strings.TrimSpace(email))
	if err != nil {
		return fmt.Errorf("store: verify subscriber: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := s.GetSubscriber(ctx, email); err != nil {
			return err
		}
	}
	return nil
}

// DeleteSubscriber removes one address.
func (s *Store) DeleteSubscriber(ctx context.Context, email string) error {
	if _, err := s.db.ExecContext(ctx,
		"DELETE FROM subscribers WHERE email = ?", strings.TrimSpace(email)); err != nil {
		return fmt.Errorf("store: delete subscriber: %w", err)
	}
	return nil
}

// DeleteUnverified removes everyone who never confirmed, and says how
// many went: the admin screen's `nukeunverified` (PLAN §9 A15).
func (s *Store) DeleteUnverified(ctx context.Context) (int, error) {
	res, err := s.db.ExecContext(ctx, "DELETE FROM subscribers WHERE verified = 0")
	if err != nil {
		return 0, fmt.Errorf("store: delete unverified subscribers: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// RemoveSubscriberByToken is the unsubscribe link's form: the address and
// its token must agree. It reports whether a row went.
func (s *Store) RemoveSubscriberByToken(ctx context.Context, email, token string) (bool, error) {
	email, token = strings.TrimSpace(email), strings.TrimSpace(token)
	if email == "" || token == "" {
		return false, nil
	}
	res, err := s.db.ExecContext(ctx,
		"DELETE FROM subscribers WHERE email = ? AND token = ?", email, token)
	if err != nil {
		return false, fmt.Errorf("store: remove subscriber by token: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}
