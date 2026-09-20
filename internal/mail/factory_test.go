package mail

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// TestMailModeDefaultsToLogEvenWithSMTPSet is the user's hard rule,
// 2026-09-19: no real mail may ever leave the container to anyone's inbox
// by default. SMTP_* being present is not consent. Only MAIL_MODE=smtp
// builds an SMTPSender.
func TestMailModeDefaultsToLogEvenWithSMTPSet(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	smtpSet := MailConfig{
		Host: "smtp.example", Port: "587", User: "blog", Password: "s3cret",
		From: "owner@blog.example",
	}

	for _, mode := range []string{"", "log", "LOG", "none", "SMTP_HOST", "smtps", " smt p", "true", "1"} {
		cfg := smtpSet
		cfg.Mode = mode
		switch got := New(cfg, logger).(type) {
		case LogSender:
		default:
			t.Fatalf("MAIL_MODE=%q with SMTP_* set gave %T; only %q may send", mode, got, ModeSMTP)
		}
	}

	if !strings.Contains(buf.String(), "never sent") {
		t.Errorf("configured-but-unused SMTP should say so in the log:\n%s", buf.String())
	}
}

// TestMailModeSMTPBuildsTheSender: the explicit opt-in, and the
// misconfiguration that falls back rather than failing a comment.
func TestMailModeSMTPBuildsTheSender(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))

	s, ok := New(MailConfig{
		Mode: "smtp", Host: "smtp.example", Port: "587",
		User: "blog", Password: "s3cret", From: "owner@blog.example",
	}, logger).(SMTPSender)
	if !ok {
		t.Fatal("MAIL_MODE=smtp must give an SMTPSender")
	}
	if s.Host != "smtp.example" || s.Port != "587" || s.User != "blog" || s.Password != "s3cret" || s.From != "owner@blog.example" {
		t.Errorf("sender = %+v", s)
	}

	// Case and stray space are forgiven on the opt-in itself.
	if _, ok := New(MailConfig{Mode: " SMTP ", Host: "smtp.example"}, logger).(SMTPSender); !ok {
		t.Error(`" SMTP " must be read as smtp`)
	}

	var buf bytes.Buffer
	if _, ok := New(MailConfig{Mode: "smtp"}, slog.New(slog.NewTextHandler(&buf, nil))).(LogSender); !ok {
		t.Error("MAIL_MODE=smtp with no SMTP_HOST must fall back to the log sender")
	}
	if !strings.Contains(buf.String(), "SMTP_HOST is empty") {
		t.Errorf("the misconfiguration must be logged:\n%s", buf.String())
	}

	// A nil logger is allowed; the default logger takes over.
	if _, ok := New(MailConfig{}, nil).(LogSender); !ok {
		t.Error("New(nil logger) must still give a sender")
	}
}
