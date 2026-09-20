// Package mail is the one way the blog sends a message (PLAN §11 "Mail"):
// a Sender interface, a log sender for when SMTP is not configured (the
// demo), and a recorder for tests. The SMTP sender arrives with M3.
package mail

import (
	"context"
	"log/slog"
	"sync"
)

// Message is one outbound mail. Bodies are plain text unless HTML is set.
type Message struct {
	To      []string
	Cc      []string
	From    string
	Subject string
	Body    string
	HTML    bool
}

// Sender delivers a message. Implementations must be safe for concurrent use.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// LogSender writes every message to the log instead of sending it: the
// demo runs without SMTP and the collector is where the mail shows up.
type LogSender struct {
	Logger *slog.Logger
}

// Send logs the message at INFO with its headers and body and never fails.
func (s LogSender) Send(_ context.Context, m Message) error {
	l := s.Logger
	if l == nil {
		l = slog.Default()
	}
	l.Info("mail (not sent: no SMTP configured)", "to", m.To, "cc", m.Cc, "from", m.From, "subject", m.Subject, "body", m.Body)
	return nil
}

// Recorder keeps every message it is given, for tests.
type Recorder struct {
	mu   sync.Mutex
	sent []Message
	Err  error // returned by Send when set
}

// Send records the message.
func (r *Recorder) Send(_ context.Context, m Message) error {
	if r.Err != nil {
		return r.Err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, m)
	return nil
}

// Messages returns a copy of what was sent, in order.
func (r *Recorder) Messages() []Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Message(nil), r.sent...)
}

// Reset forgets what was sent.
func (r *Recorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = nil
}
