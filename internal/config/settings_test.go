package config_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/4scottt/go-blogcfc/internal/config"
	"github.com/4scottt/go-blogcfc/internal/store"
	"github.com/4scottt/go-blogcfc/internal/testdb"
)

func newSettings(t *testing.T) (*config.Settings, *config.Config, *store.Store) {
	t.Helper()
	st := testdb.New(t)
	cfg := &config.Config{Port: 8081, BlogBaseURL: "http://localhost:8081"}
	s := config.NewSettings(st, cfg)
	if err := s.Reload(context.Background()); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	return s, cfg, st
}

// TestSettingsDefaultsAfterSeed checks every §10 key the seed
// promises, in its typed form.
func TestSettingsDefaultsAfterSeed(t *testing.T) {
	s, cfg, _ := newSettings(t)

	if got := s.BlogTitle(); got != "BlogCFC" {
		t.Errorf("blogtitle = %q", got)
	}
	if got := s.MaxEntries(); got != 10 {
		t.Errorf("maxentries = %d, want 10", got)
	}
	if got := s.MaxEntriesAdmin(); got != 20 {
		t.Errorf("maxentriesadmin = %d, want 20", got)
	}
	if got := s.Locale(); got != "en_US" {
		t.Errorf("locale = %q, want en_US", got)
	}
	for _, c := range []struct {
		name string
		got  bool
		want bool
	}{
		{"moderate", s.Moderate(), true},
		{"usecaptcha", s.UseCaptcha(), true},
		{"usecfp", s.UseCFP(), true},
		{"usetweetbacks", s.UseTweetbacks(), false},
		{"allowgravatars", s.AllowGravatars(), true},
		{"filebrowse", s.FileBrowse(), true},
		{"itunesexplicit", s.ITunesExplicit(), false},
	} {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
	if s.Timezone() != time.UTC {
		t.Errorf("timezone = %v, want UTC", s.Timezone())
	}
	// The spam list is BlogCFC's, one term per line.
	spam := s.TrackbackSpamList()
	if len(spam) < 200 {
		t.Errorf("trackbackspamlist has %d terms, want BlogCFC's whole list", len(spam))
	}
	if spam[0] != "-insurance" {
		t.Errorf("first spam term = %q", spam[0])
	}
	for _, term := range spam {
		if strings.Contains(term, ",") {
			t.Fatalf("spam term %q still holds a comma: the list is one term per line", term)
		}
	}
	if pods := s.Pods(); len(pods) != 5 || pods[0].Name != "calendar" || !pods[0].Show {
		t.Errorf("pods = %+v, want the five defaults with the calendar first", pods)
	}
	// blogurl is never stored; it mirrors BLOG_BASE_URL.
	if s.BlogURL() != cfg.BlogBaseURL || s.String("blogurl") != cfg.BlogBaseURL {
		t.Errorf("blogurl = %q, want %q", s.BlogURL(), cfg.BlogBaseURL)
	}
	if all := s.All(); all["blogurl"] != cfg.BlogBaseURL {
		t.Errorf("All()[blogurl] = %q", all["blogurl"])
	}
}

// TestSettingsSetRoundTrip writes through the accessor and reads
// the typed getters back, including the newline lists.
func TestSettingsSetRoundTrip(t *testing.T) {
	s, cfg, st := newSettings(t)
	ctx := context.Background()

	err := s.Set(ctx, map[string]string{
		"blogtitle":      "The Rewrite",
		"maxentries":     "3",
		"moderate":       "no",
		"usecaptcha":     "YES",
		"pingurls":       "http://one.example/ping\n\nhttp://two.example/ping\n",
		"ipblocklist":    "10.0.0.1\n192.168.*",
		"owneremail":     "owner@example.test",
		"blogurl":        "http://tampered.example",
		"itunesexplicit": "yes",
	})
	if err != nil {
		t.Fatalf("Set: %v", err)
	}

	if got := s.BlogTitle(); got != "The Rewrite" {
		t.Errorf("blogtitle = %q", got)
	}
	if got := s.MaxEntries(); got != 3 {
		t.Errorf("maxentries = %d", got)
	}
	if s.Moderate() {
		t.Error("moderate = yes after being set to no")
	}
	if !s.UseCaptcha() {
		t.Error("usecaptcha: YES should parse as true")
	}
	if !s.ITunesExplicit() {
		t.Error("itunesexplicit = no after being set to yes")
	}
	if got := s.PingURLs(); len(got) != 2 || got[0] != "http://one.example/ping" || got[1] != "http://two.example/ping" {
		t.Errorf("pingurls = %#v, blank lines should go", got)
	}
	if got := s.IPBlockList(); len(got) != 2 || got[1] != "192.168.*" {
		t.Errorf("ipblocklist = %#v", got)
	}
	// blogurl is refused even when a form posts it.
	if s.BlogURL() != cfg.BlogBaseURL {
		t.Errorf("blogurl was overwritten: %q", s.BlogURL())
	}

	// A fresh accessor over the same table sees the writes: the values
	// went to the database, not only to the cache.
	fresh := config.NewSettings(st, cfg)
	if err := fresh.Reload(ctx); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if fresh.BlogTitle() != "The Rewrite" || fresh.MaxEntries() != 3 || fresh.Moderate() {
		t.Errorf("a fresh accessor read %q / %d / %v", fresh.BlogTitle(), fresh.MaxEntries(), fresh.Moderate())
	}
}

// TestSettingsBadTimezoneFallsBackToUTC: a typo must not take the
// blog down; it logs and uses UTC.
func TestSettingsBadTimezoneFallsBackToUTC(t *testing.T) {
	s, _, _ := newSettings(t)
	ctx := context.Background()

	if err := s.Set(ctx, map[string]string{"timezone": "America/Los_Angeles"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got := s.Timezone().String(); got != "America/Los_Angeles" {
		t.Fatalf("timezone = %q, want America/Los_Angeles", got)
	}

	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	defer slog.SetDefault(old)

	if err := s.Set(ctx, map[string]string{"timezone": "Mars/Olympus_Mons"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if s.Timezone() != time.UTC {
		t.Errorf("timezone = %v, want the UTC fallback", s.Timezone())
	}
	if !strings.Contains(buf.String(), "Mars/Olympus_Mons") {
		t.Errorf("no warning logged for the bad zone: %q", buf.String())
	}

	// An unparsable number warns and keeps the caller's default too.
	buf.Reset()
	if err := s.Set(ctx, map[string]string{"maxentries": "ten"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got := s.MaxEntries(); got != 10 {
		t.Errorf("maxentries = %d, want the default 10", got)
	}
	if !strings.Contains(buf.String(), "maxentries") {
		t.Errorf("no warning logged for the bad number: %q", buf.String())
	}
}

// TestSettingsPodsJSON round-trips the pods list.
func TestSettingsPodsJSON(t *testing.T) {
	s, _, _ := newSettings(t)
	ctx := context.Background()

	pods := []config.Pod{
		{Name: "recent", Show: true, Order: 2},
		{Name: "calendar", Show: false, Order: 1},
	}
	if err := s.SetPods(ctx, pods); err != nil {
		t.Fatalf("SetPods: %v", err)
	}
	got := s.Pods()
	if len(got) != 2 || got[0].Name != "calendar" || got[0].Show || got[1].Name != "recent" {
		t.Fatalf("pods = %+v, want them in order", got)
	}

	var raw []config.Pod
	if err := json.Unmarshal([]byte(s.String("pods")), &raw); err != nil {
		t.Fatalf("the stored pods value is not JSON: %v", err)
	}
}
