package mail

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// SMTPSender delivers over SMTP: STARTTLS when the server offers it,
// PLAIN auth when a user is configured, RFC 5322 headers.
//
// Nothing constructs one unless MAIL_MODE is explicitly `smtp` (see
// New): the default everywhere, including a deployed demo with SMTP_*
// set, is the log sender, so no mail can reach an inbox by accident.
type SMTPSender struct {
	Host     string
	Port     string // default "25"
	User     string
	Password string

	// From is the envelope sender used when a Message has none.
	From string

	// now and dialer exist for the tests; both are optional.
	now    func() time.Time
	dialer *net.Dialer
}

// smtpTimeout bounds a send when the caller's context has no deadline of
// its own: a blog request must not hang on a sulking mail server.
const smtpTimeout = 30 * time.Second

var (
	errNoHost = errors.New("mail: SMTP_HOST is empty")
	errNoFrom = errors.New("mail: message has no From and no default")
	errNoTo   = errors.New("mail: message has no recipients")
)

// Send delivers one message. It is safe for concurrent use: every call
// opens its own connection.
func (s SMTPSender) Send(ctx context.Context, m Message) error {
	if s.Host == "" {
		return errNoHost
	}
	from := m.From
	if from == "" {
		from = s.From
	}
	if from == "" {
		return errNoFrom
	}
	rcpts := append(append([]string{}, m.To...), m.Cc...)
	if len(rcpts) == 0 {
		return errNoTo
	}

	port := s.Port
	if port == "" {
		port = "25"
	}
	addr := net.JoinHostPort(s.Host, port)

	ctx, cancel := context.WithTimeout(ctx, smtpTimeout)
	defer cancel()

	d := s.dialer
	if d == nil {
		d = &net.Dialer{}
	}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("mail: dial %s: %w", addr, err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	c, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("mail: smtp %s: %w", addr, err)
	}
	defer func() { _ = c.Close() }()

	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("mail: starttls: %w", err)
		}
	}
	if s.User != "" {
		if ok, _ := c.Extension("AUTH"); !ok {
			return errors.New("mail: SMTP_USER is set but the server offers no AUTH")
		}
		if err := c.Auth(smtp.PlainAuth("", s.User, s.Password, s.Host)); err != nil {
			return fmt.Errorf("mail: auth: %w", err)
		}
	}

	if err := c.Mail(from); err != nil {
		return fmt.Errorf("mail: MAIL FROM: %w", err)
	}
	for _, to := range rcpts {
		if err := c.Rcpt(to); err != nil {
			return fmt.Errorf("mail: RCPT TO %s: %w", to, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("mail: DATA: %w", err)
	}
	if _, err := w.Write(s.render(m, from)); err != nil {
		_ = w.Close()
		return fmt.Errorf("mail: write body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("mail: end of data: %w", err)
	}
	return c.Quit()
}

// render builds the RFC 5322 message: the headers a mail server and a
// reader expect, then the body with CRLF line endings.
func (s SMTPSender) render(m Message, from string) []byte {
	now := time.Now
	if s.now != nil {
		now = s.now
	}
	t := now()

	var b strings.Builder
	header := func(k, v string) {
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(v)
		b.WriteString("\r\n")
	}
	header("Date", t.Format(time.RFC1123Z))
	header("From", from)
	if len(m.To) > 0 {
		header("To", strings.Join(m.To, ", "))
	}
	if len(m.Cc) > 0 {
		header("Cc", strings.Join(m.Cc, ", "))
	}
	header("Subject", encodeHeader(m.Subject))
	header("Message-ID", messageID(from, t))
	header("MIME-Version", "1.0")
	if m.HTML {
		header("Content-Type", `text/html; charset="utf-8"`)
	} else {
		header("Content-Type", `text/plain; charset="utf-8"`)
	}
	header("Content-Transfer-Encoding", "8bit")
	b.WriteString("\r\n")
	b.WriteString(crlf(m.Body))
	if !strings.HasSuffix(m.Body, "\n") {
		b.WriteString("\r\n")
	}
	return []byte(b.String())
}

// encodeHeader MIME-encodes a header value when it is not plain ASCII,
// so a subject with an umlaut in it survives the wire.
func encodeHeader(v string) string {
	v = strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' {
			return -1 // never let a value forge a header
		}
		return r
	}, v)
	for _, r := range v {
		if r > 127 {
			return mime.QEncoding.Encode("utf-8", v)
		}
	}
	return v
}

// messageID is `<nanos.random@domain>`, the domain taken from the sender.
func messageID(from string, t time.Time) string {
	domain := "localhost"
	if _, d, ok := strings.Cut(from, "@"); ok && d != "" {
		domain = strings.Trim(d, "<> ")
	}
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("<%d@%s>", t.UnixNano(), domain)
	}
	return fmt.Sprintf("<%d.%s@%s>", t.UnixNano(), hex.EncodeToString(buf[:]), domain)
}

// crlf normalises line endings; net/smtp's writer does the dot-stuffing.
func crlf(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n")
}
