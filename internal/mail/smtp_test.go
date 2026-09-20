package mail

import (
	"bufio"
	"context"
	"encoding/base64"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSMTP is just enough of an SMTP server to prove the sender speaks
// it: a greeting, EHLO, optional AUTH PLAIN, MAIL, RCPT, DATA, QUIT. No
// TLS, and it never leaves 127.0.0.1 - the whole point of the exercise is
// that the test sends no mail anywhere.
type fakeSMTP struct {
	ln       net.Listener
	withAuth bool

	mu   sync.Mutex
	done chan struct{}

	From string
	To   []string
	Data string
	Auth string
}

func startFakeSMTP(t *testing.T, withAuth bool) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &fakeSMTP{ln: ln, withAuth: withAuth, done: make(chan struct{})}
	go s.accept()
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

func (s *fakeSMTP) addr() (host, port string) {
	h, p, _ := net.SplitHostPort(s.ln.Addr().String())
	return h, p
}

func (s *fakeSMTP) accept() {
	conn, err := s.ln.Accept()
	if err != nil {
		return
	}
	defer func() {
		_ = conn.Close()
		close(s.done)
	}()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	say := func(line string) {
		_, _ = w.WriteString(line + "\r\n")
		_ = w.Flush()
	}

	say("220 fake.example ESMTP ready")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		verb, rest, _ := strings.Cut(line, " ")
		switch strings.ToUpper(verb) {
		case "EHLO", "HELO":
			if s.withAuth {
				say("250-fake.example")
				say("250 AUTH PLAIN")
			} else {
				say("250 fake.example")
			}
		case "AUTH":
			_, payload, _ := strings.Cut(rest, " ")
			raw, _ := base64.StdEncoding.DecodeString(payload)
			s.mu.Lock()
			s.Auth = strings.ReplaceAll(string(raw), "\x00", "|")
			s.mu.Unlock()
			say("235 2.7.0 Authentication successful")
		case "MAIL":
			s.mu.Lock()
			s.From = strings.Trim(strings.TrimPrefix(rest, "FROM:"), "<> ")
			s.mu.Unlock()
			say("250 2.1.0 Ok")
		case "RCPT":
			s.mu.Lock()
			s.To = append(s.To, strings.Trim(strings.TrimPrefix(rest, "TO:"), "<> "))
			s.mu.Unlock()
			say("250 2.1.5 Ok")
		case "DATA":
			say("354 End data with <CR><LF>.<CR><LF>")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" || l == ".\n" {
					break
				}
				b.WriteString(strings.TrimPrefix(l, ".")) // undo dot-stuffing
			}
			s.mu.Lock()
			s.Data = b.String()
			s.mu.Unlock()
			say("250 2.0.0 Ok: queued")
		case "QUIT":
			say("221 2.0.0 Bye")
			return
		default:
			say("250 2.0.0 Ok")
		}
	}
}

func (s *fakeSMTP) wait(t *testing.T) {
	t.Helper()
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the fake server never finished the session")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
}

// TestSMTPSenderEnvelopeAndBody drives the SMTP sender against the fake
// server and checks the envelope and the RFC 5322 message.
func TestSMTPSenderEnvelopeAndBody(t *testing.T) {
	srv := startFakeSMTP(t, false)
	host, port := srv.addr()

	s := SMTPSender{Host: host, Port: port, From: "fallback@blog.example"}
	err := s.Send(context.Background(), Message{
		To:      []string{"bob@example.com", "carol@example.com"},
		Cc:      []string{"owner@example.com"},
		From:    "comments@blog.example",
		Subject: "Comment posted to Ray's Blog : On Tags",
		Body:    "First!\n\n.A line that needs stuffing\n",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	srv.wait(t)

	if srv.From != "comments@blog.example" {
		t.Errorf("MAIL FROM = %q", srv.From)
	}
	want := []string{"bob@example.com", "carol@example.com", "owner@example.com"}
	if strings.Join(srv.To, ",") != strings.Join(want, ",") {
		t.Errorf("RCPT TO = %v, want %v (To then Cc)", srv.To, want)
	}
	if srv.Auth != "" {
		t.Errorf("authenticated without a user: %q", srv.Auth)
	}

	headers, body, ok := strings.Cut(srv.Data, "\r\n\r\n")
	if !ok {
		t.Fatalf("no header/body split in:\n%q", srv.Data)
	}
	for _, want := range []string{
		"From: comments@blog.example",
		"To: bob@example.com, carol@example.com",
		"Cc: owner@example.com",
		"Subject: Comment posted to Ray's Blog : On Tags",
		"MIME-Version: 1.0",
		`Content-Type: text/plain; charset="utf-8"`,
		"Date: ",
		"Message-ID: <",
		"@blog.example>",
	} {
		if !strings.Contains(headers, want) {
			t.Errorf("headers miss %q:\n%s", want, headers)
		}
	}
	if want := "First!\r\n\r\n.A line that needs stuffing\r\n"; body != want {
		t.Errorf("body = %q, want %q", body, want)
	}
}

// TestSMTPSenderAuthAndHTML covers PLAIN auth when a user is set and the
// HTML content type.
func TestSMTPSenderAuthAndHTML(t *testing.T) {
	srv := startFakeSMTP(t, true)
	host, port := srv.addr()

	s := SMTPSender{Host: host, Port: port, User: "blog", Password: "s3cret"}
	err := s.Send(context.Background(), Message{
		To:      []string{"bob@example.com"},
		From:    "owner@blog.example",
		Subject: "Grüße",
		Body:    "<p>hi</p>",
		HTML:    true,
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	srv.wait(t)

	if srv.Auth != "|blog|s3cret" {
		t.Errorf("AUTH PLAIN payload = %q, want %q", srv.Auth, "|blog|s3cret")
	}
	if !strings.Contains(srv.Data, `Content-Type: text/html; charset="utf-8"`) {
		t.Errorf("HTML content type missing:\n%s", srv.Data)
	}
	// A non-ASCII subject is MIME-encoded, not sent raw.
	if strings.Contains(srv.Data, "Subject: Grüße") {
		t.Error("a non-ASCII subject must be encoded")
	}
	if !strings.Contains(srv.Data, "Subject: =?utf-8?q?") {
		t.Errorf("no encoded-word subject:\n%s", srv.Data)
	}
}

// TestSMTPSenderRefusesIncompleteMessages: nothing is dialled without a
// host, a sender and a recipient.
func TestSMTPSenderRefusesIncompleteMessages(t *testing.T) {
	for name, tc := range map[string]struct {
		s SMTPSender
		m Message
	}{
		"no host":      {SMTPSender{}, Message{To: []string{"a@b"}, From: "c@d"}},
		"no from":      {SMTPSender{Host: "127.0.0.1"}, Message{To: []string{"a@b"}}},
		"no recipient": {SMTPSender{Host: "127.0.0.1"}, Message{From: "c@d"}},
	} {
		if err := tc.s.Send(context.Background(), tc.m); err == nil {
			t.Errorf("%s: Send returned nil", name)
		}
	}
}

// TestHeaderInjectionIsRefused: a newline in a subject must not become a
// header of its own.
func TestHeaderInjectionIsRefused(t *testing.T) {
	s := SMTPSender{Host: "127.0.0.1"}
	got := string(s.render(Message{
		To:      []string{"bob@example.com"},
		Subject: "hello\r\nBcc: victim@example.com",
		Body:    "hi",
	}, "owner@blog.example"))
	if strings.Contains(got, "\r\nBcc:") {
		t.Errorf("a subject forged a header:\n%s", got)
	}
	if !strings.Contains(got, "Subject: helloBcc: victim@example.com") {
		t.Errorf("subject = unexpected:\n%s", got)
	}
}
