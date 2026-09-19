package config

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SettingsStore is the slice of the store the accessor needs. *store.Store
// satisfies it; a test may fake it.
type SettingsStore interface {
	AllSettings(ctx context.Context) (map[string]string, error)
	SetSettings(ctx context.Context, values map[string]string) error
}

// Pod is one sidebar widget's entry in the `pods` setting.
type Pod struct {
	Name  string `json:"name"`
	Show  bool   `json:"show"`
	Order int    `json:"order"`
}

// Settings reads the settings table through an in-memory cache. Every
// getter is safe for concurrent use; a write goes through Set, which
// reloads the cache.
type Settings struct {
	st  SettingsStore
	cfg *Config

	mu     sync.RWMutex
	values map[string]string
	loc    *time.Location
}

// NewSettings builds an accessor. Call Reload before serving.
func NewSettings(st SettingsStore, cfg *Config) *Settings {
	return &Settings{st: st, cfg: cfg, values: map[string]string{}, loc: time.UTC}
}

// Reload re-reads the settings table into the cache.
func (s *Settings) Reload(ctx context.Context) error {
	values, err := s.st.AllSettings(ctx)
	if err != nil {
		return err
	}
	loc := parseLocation(values["timezone"])
	s.mu.Lock()
	s.values, s.loc = values, loc
	s.mu.Unlock()
	return nil
}

// Set writes the given keys and reloads the cache. `blogurl` is ignored:
// it mirrors BLOG_BASE_URL and is never stored.
func (s *Settings) Set(ctx context.Context, values map[string]string) error {
	write := make(map[string]string, len(values))
	for k, v := range values {
		k = strings.ToLower(strings.TrimSpace(k))
		if k == "" || k == "blogurl" {
			continue
		}
		write[k] = v
	}
	if err := s.st.SetSettings(ctx, write); err != nil {
		return err
	}
	return s.Reload(ctx)
}

// All returns a copy of the cached settings, with `blogurl` filled in from
// the environment so a Settings page can show it read-only.
func (s *Settings) All() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.values)+1)
	for k, v := range s.values {
		out[k] = v
	}
	out["blogurl"] = s.cfg.BlogBaseURL
	return out
}

// String returns a raw setting; `blogurl` comes from BLOG_BASE_URL.
func (s *Settings) String(key string) string {
	if key == "blogurl" {
		return s.cfg.BlogBaseURL
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.values[key]
}

// Int returns a numeric setting, or def when it is missing or unparsable.
func (s *Settings) Int(key string, def int) int {
	v := strings.TrimSpace(s.String(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		slog.Warn("setting is not a number, using the default", "key", key, "value", v, "default", def)
		return def
	}
	return n
}

// Bool reads BlogCFC's yes/no settings; anything unrecognised is false.
func (s *Settings) Bool(key string) bool {
	switch strings.ToLower(strings.TrimSpace(s.String(key))) {
	case "yes", "true", "1", "on":
		return true
	default:
		return false
	}
}

// List splits a newline list setting, dropping blanks and trimming.
func (s *Settings) List(key string) []string {
	raw := s.String(key)
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		if t := strings.TrimSpace(line); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// Blog information.

func (s *Settings) BlogTitle() string       { return s.String("blogtitle") }
func (s *Settings) BlogDescription() string { return s.String("blogdescription") }
func (s *Settings) BlogKeywords() string    { return s.String("blogkeywords") }
func (s *Settings) OwnerEmail() string      { return s.String("owneremail") }
func (s *Settings) FailTo() string          { return s.String("failto") }

// BlogURL is not stored: every link is built from BLOG_BASE_URL.
func (s *Settings) BlogURL() string { return s.cfg.BlogBaseURL }

// Content.

func (s *Settings) CommentsFrom() string { return s.String("commentsfrom") }
func (s *Settings) MaxEntries() int      { return s.Int("maxentries", 10) }
func (s *Settings) MaxEntriesAdmin() int { return s.Int("maxentriesadmin", 20) }
func (s *Settings) PingURLs() []string   { return s.List("pingurls") }
func (s *Settings) Locale() string       { return orDefault(s.String("locale"), "en_US") }

// Timezone is the blog's display zone. A zone the host cannot load falls
// back to UTC with a warning, so a typo never stops the blog.
func (s *Settings) Timezone() *time.Location {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.loc == nil {
		return time.UTC
	}
	return s.loc
}

// Content controls and security.

func (s *Settings) IPBlockList() []string       { return s.List("ipblocklist") }
func (s *Settings) Moderate() bool              { return s.Bool("moderate") }
func (s *Settings) UseCaptcha() bool            { return s.Bool("usecaptcha") }
func (s *Settings) UseCFP() bool                { return s.Bool("usecfp") }
func (s *Settings) UseTweetbacks() bool         { return s.Bool("usetweetbacks") }
func (s *Settings) TrackbackSpamList() []string { return s.List("trackbackspamlist") }
func (s *Settings) AllowGravatars() bool        { return s.Bool("allowgravatars") }
func (s *Settings) FileBrowse() bool            { return s.Bool("filebrowse") }
func (s *Settings) ImageRoot() string           { return s.String("imageroot") }

// Podcasting.

func (s *Settings) ITunesSubtitle() string { return s.String("itunessubtitle") }
func (s *Settings) ITunesSummary() string  { return s.String("itunessummary") }
func (s *Settings) ITunesKeywords() string { return s.String("ituneskeywords") }
func (s *Settings) ITunesAuthor() string   { return s.String("itunesauthor") }
func (s *Settings) ITunesImage() string    { return s.String("itunesimage") }
func (s *Settings) ITunesExplicit() bool   { return s.Bool("itunesexplicit") }

// Pods returns the sidebar widgets in order. Unparsable JSON logs a warning
// and yields no pods rather than breaking the page.
func (s *Settings) Pods() []Pod {
	raw := strings.TrimSpace(s.String("pods"))
	if raw == "" {
		return nil
	}
	var pods []Pod
	if err := json.Unmarshal([]byte(raw), &pods); err != nil {
		slog.Warn("pods setting is not valid JSON", "error", err)
		return nil
	}
	sort.SliceStable(pods, func(i, j int) bool { return pods[i].Order < pods[j].Order })
	return pods
}

// SetPods writes the pods setting.
func (s *Settings) SetPods(ctx context.Context, pods []Pod) error {
	b, err := json.Marshal(pods)
	if err != nil {
		return fmt.Errorf("config: pods: %w", err)
	}
	return s.Set(ctx, map[string]string{"pods": string(b)})
}

func parseLocation(name string) *time.Location {
	name = strings.TrimSpace(name)
	if name == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		slog.Warn("timezone setting is not a known IANA zone, using UTC", "timezone", name, "error", err)
		return time.UTC
	}
	return loc
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}
