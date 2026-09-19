package config_test

import (
	"strings"
	"testing"

	"github.com/4scottt/go-blogcfc/internal/config"
)

// TestConfigDefaultsAndRequiredForServe covers the §6 table: the
// defaults, and what serving insists on.
func TestConfigDefaultsAndRequiredForServe(t *testing.T) {
	for _, k := range []string{"PORT", "BLOG_BASE_URL", "SESSION_SECRET", "DATA_DIR", "DB_HOST", "DB_PORT", "DB_NAME", "DB_USER", "DB_PASSWORD", "ADMIN_PASSWORD", "TZ"} {
		t.Setenv(k, "")
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != 8080 {
		t.Errorf("PORT default = %d, want 8080", cfg.Port)
	}
	if cfg.DataDir != "/var/lib/go-blogcfc" {
		t.Errorf("DATA_DIR default = %q", cfg.DataDir)
	}
	if cfg.MailConfigured() {
		t.Error("mail must be unconfigured without SMTP_HOST")
	}
	err = cfg.ValidateForServe()
	if err == nil {
		t.Fatal("serve must refuse to start without BLOG_BASE_URL and SESSION_SECRET")
	}
	for _, want := range []string{"BLOG_BASE_URL", "SESSION_SECRET"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %s", err, want)
		}
	}

	t.Setenv("PORT", "8081")
	t.Setenv("BLOG_BASE_URL", "http://localhost:8081/")
	t.Setenv("SESSION_SECRET", "dev-only-not-secret")
	t.Setenv("DB_HOST", "db")
	t.Setenv("DB_PORT", "3306")
	t.Setenv("DB_NAME", "goblogcfc")
	t.Setenv("DB_USER", "goblogcfc")
	t.Setenv("DB_PASSWORD", "secret")

	cfg, err = config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := cfg.ValidateForServe(); err != nil {
		t.Fatalf("ValidateForServe: %v", err)
	}
	if cfg.BlogBaseURL != "http://localhost:8081" {
		t.Errorf("BLOG_BASE_URL = %q, the trailing slash should go", cfg.BlogBaseURL)
	}
	if cfg.Addr() != ":8081" {
		t.Errorf("Addr = %q", cfg.Addr())
	}
	want := "goblogcfc:secret@tcp(db:3306)/goblogcfc?parseTime=true&loc=UTC&charset=utf8mb4"
	if cfg.DSN() != want {
		t.Errorf("DSN = %q, want %q", cfg.DSN(), want)
	}

	t.Setenv("BLOG_BASE_URL", "not-a-url")
	cfg, _ = config.Load()
	if err := cfg.ValidateForServe(); err == nil {
		t.Error("a relative BLOG_BASE_URL must be refused")
	}
}
