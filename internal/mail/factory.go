package mail

import (
	"log/slog"
	"strings"
)

// MailConfig is the process's mail settings, read from the environment
// by config.Config.Mail(). It lives here, not in config, so that this
// package never imports config.
type MailConfig struct {
	// Mode is MAIL_MODE. Only the exact value "smtp" sends; anything
	// else - including "", "log", and "smtp " with a typo - logs.
	Mode string

	Host     string
	Port     string
	User     string
	Password string

	// From is the fallback envelope sender for a message with none.
	From string
}

// ModeSMTP is the one value of MAIL_MODE that sends real mail.
const ModeSMTP = "smtp"

// ModeLog is the default: every message is written to the log and
// nothing leaves the container.
const ModeLog = "log"

// New returns the process's sender.
//
// The rule this encodes (the user's, 2026-09-19): no real mail may ever
// leave the container to anyone's inbox by default. SMTP_* being set is
// not consent; only MAIL_MODE=smtp is. Anything else - unset, "log", a
// misspelling - gets the log sender, and a MAIL_MODE=smtp with no
// SMTP_HOST is a misconfiguration that also falls back to logging rather
// than failing a comment at the worst moment.
func New(cfg MailConfig, logger *slog.Logger) Sender {
	if logger == nil {
		logger = slog.Default()
	}
	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	if mode != ModeSMTP {
		if cfg.Host != "" {
			logger.Info("mail: SMTP is configured but MAIL_MODE is not smtp; mail will be logged, never sent",
				"mail_mode", cfg.Mode, "smtp_host", cfg.Host)
		}
		return LogSender{Logger: logger}
	}
	if cfg.Host == "" {
		logger.Error("mail: MAIL_MODE=smtp but SMTP_HOST is empty; falling back to the log sender")
		return LogSender{Logger: logger}
	}
	logger.Warn("mail: MAIL_MODE=smtp - mail will be sent for real", "smtp_host", cfg.Host, "smtp_port", cfg.Port)
	return SMTPSender{
		Host:     cfg.Host,
		Port:     cfg.Port,
		User:     cfg.User,
		Password: cfg.Password,
		From:     cfg.From,
	}
}
