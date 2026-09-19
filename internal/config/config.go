// Package config reads the runtime environment (PLAN §6) and serves the
// settings table through a typed, cached accessor (PLAN §10).
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// Config is the environment the process starts with. Everything here is
// read once at start; the settings table holds what an operator may change
// while the blog runs.
type Config struct {
	Port        int
	BlogBaseURL string

	DBHost     string
	DBPort     int
	DBName     string
	DBUser     string
	DBPassword string

	AdminPassword string
	SessionSecret string
	DataDir       string

	SMTPHost     string
	SMTPPort     int
	SMTPUser     string
	SMTPPassword string

	TZ string
}

// Load reads the environment. It validates only what every subcommand
// needs; ValidateForServe adds what serving needs.
func Load() (*Config, error) {
	c := &Config{
		Port:          envInt("PORT", 8080),
		BlogBaseURL:   strings.TrimRight(os.Getenv("BLOG_BASE_URL"), "/"),
		DBHost:        envString("DB_HOST", "127.0.0.1"),
		DBPort:        envInt("DB_PORT", 3306),
		DBName:        envString("DB_NAME", "goblogcfc"),
		DBUser:        envString("DB_USER", "goblogcfc"),
		DBPassword:    os.Getenv("DB_PASSWORD"),
		AdminPassword: os.Getenv("ADMIN_PASSWORD"),
		SessionSecret: os.Getenv("SESSION_SECRET"),
		DataDir:       envString("DATA_DIR", "/var/lib/go-blogcfc"),
		SMTPHost:      os.Getenv("SMTP_HOST"),
		SMTPPort:      envInt("SMTP_PORT", 25),
		SMTPUser:      os.Getenv("SMTP_USER"),
		SMTPPassword:  os.Getenv("SMTP_PASSWORD"),
		TZ:            envString("TZ", "UTC"),
	}
	if c.Port < 1 || c.Port > 65535 {
		return nil, fmt.Errorf("config: PORT %d out of range", c.Port)
	}
	return c, nil
}

// ValidateForServe checks the variables only the server needs, so that
// `migrate` and `seed-admin` run without them.
func (c *Config) ValidateForServe() error {
	var errs []error
	if c.BlogBaseURL == "" {
		errs = append(errs, errors.New("BLOG_BASE_URL is required (every link, feed and mail is built from it)"))
	} else if u, err := url.Parse(c.BlogBaseURL); err != nil || u.Scheme == "" || u.Host == "" {
		errs = append(errs, fmt.Errorf("BLOG_BASE_URL %q is not an absolute URL", c.BlogBaseURL))
	}
	if c.SessionSecret == "" {
		errs = append(errs, errors.New("SESSION_SECRET is required (it signs the session cookie)"))
	}
	if len(errs) > 0 {
		return fmt.Errorf("config: %w", errors.Join(errs...))
	}
	return nil
}

// DSN is the MariaDB connection string: times parsed as time.Time in UTC,
// utf8mb4 throughout.
func (c *Config) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true&loc=UTC&charset=utf8mb4",
		c.DBUser, c.DBPassword, c.DBHost, c.DBPort, c.DBName)
}

// Addr is the listen address.
func (c *Config) Addr() string { return fmt.Sprintf(":%d", c.Port) }

// MailConfigured reports whether SMTP is set; when it is not, the mail
// sender logs instead of sending.
func (c *Config) MailConfigured() bool { return c.SMTPHost != "" }

func envString(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return def
}
