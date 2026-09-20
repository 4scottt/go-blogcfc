package web_test

// This file is the one external test in the package: it needs the feed and
// sitemap handlers, and internal/feeds imports internal/web.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/4scottt/go-blogcfc/internal/config"
	"github.com/4scottt/go-blogcfc/internal/feeds"
	"github.com/4scottt/go-blogcfc/internal/mail"
	"github.com/4scottt/go-blogcfc/internal/notify"
	"github.com/4scottt/go-blogcfc/internal/store"
	"github.com/4scottt/go-blogcfc/internal/testdb"
	"github.com/4scottt/go-blogcfc/internal/web"
)

// base is what every link must be built from; evilHost is what a request
// says it is. On the platform the two differ for real: the health probe
// and the browser worker arrive as 127.0.0.1 while the blog's links have
// to be the address the visitor uses (PLAN §6).
const (
	base      = "http://blog.example"
	evilHost  = "evil.example"
	evilProto = "https"
)

// nobody is an anonymous visitor.
type nobody struct{}

func (nobody) Current(*http.Request) *store.User { return nil }

// TestFP_O05_EveryURLComesFromBaseURLNotHost walks the pages, the feeds
// and a comment notification with a forged Host and X-Forwarded-Host, and
// fails if the forgery reaches any URL (PLAN §6, FP O05).
func TestFP_O05_EveryURLComesFromBaseURLNotHost(t *testing.T) {
	st := testdb.New(t)
	ctx := context.Background()
	cfg := &config.Config{BlogBaseURL: base, Port: 8080}
	settings := config.NewSettings(st, cfg)
	if err := settings.Reload(ctx); err != nil {
		t.Fatalf("settings reload: %v", err)
	}
	if err := settings.Set(ctx, map[string]string{"owneremail": "owner@blog.example"}); err != nil {
		t.Fatalf("set owneremail: %v", err)
	}

	author := &store.User{Username: "ray", Name: "Raymond Camden", PasswordHash: "$2a$10$notarealhash"}
	if err := st.CreateUser(ctx, author, nil); err != nil {
		t.Fatalf("create user: %v", err)
	}
	entry := store.Entry{
		Title:    "Hosting a blog",
		Alias:    "hosting-a-blog",
		Body:     "<p>Every link is absolute.</p>",
		Posted:   time.Date(2026, 3, 2, 12, 0, 0, 0, time.UTC),
		Username: "ray",
		Released: true,
	}
	if err := st.CreateEntry(ctx, &entry); err != nil {
		t.Fatalf("create entry: %v", err)
	}

	mux := http.NewServeMux()
	web.New(cfg, st, settings, nobody{}).Routes(mux)
	feeds.NewRSS(cfg, st, settings).Routes(mux)
	feeds.NewSitemap(cfg, st, settings).Routes(mux)

	permalink := web.EntryURL(base, entry, settings.Timezone())
	entryPath := strings.TrimPrefix(permalink, base)
	if entryPath == permalink || entryPath == "" {
		t.Fatalf("permalink %q is not under %q", permalink, base)
	}

	for _, path := range []string{"/", entryPath, "/rss", "/rss?mode=rss1", "/sitemap.xml", "/robots.txt"} {
		t.Run(path, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, path, nil)
			// A request that lies about where it arrived, in both of the
			// ways a proxy would tell us.
			r.Host = evilHost
			r.Header.Set("Host", evilHost)
			r.Header.Set("X-Forwarded-Host", evilHost)
			r.Header.Set("X-Forwarded-Proto", evilProto)

			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, r)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200", path, rec.Code)
			}
			body := rec.Body.String()
			if strings.Contains(body, evilHost) {
				t.Errorf("GET %s echoes the forged host", path)
			}
			if !strings.Contains(body, base) {
				t.Errorf("GET %s carries no %s URL at all", path, base)
			}
			for _, h := range []string{"Location", "Link"} {
				if v := rec.Header().Get(h); strings.Contains(v, evilHost) {
					t.Errorf("GET %s: %s header = %q", path, h, v)
				}
			}
		})
	}

	// The mail a comment sets off is built from the same base, never from
	// the request that posted the comment.
	t.Run("comment notification", func(t *testing.T) {
		comment := store.Comment{
			EntryID: entry.ID,
			Name:    "A reader",
			Email:   "reader@example.com",
			Comment: "Nice post.",
			Posted:  time.Date(2026, 3, 2, 13, 0, 0, 0, time.UTC),
		}
		if err := st.CreateComment(ctx, &comment); err != nil {
			t.Fatalf("create comment: %v", err)
		}
		recorder := &mail.Recorder{}
		n := notify.New(cfg, st, settings, recorder)
		n.Link = func(e store.Entry) string { return web.EntryURL(cfg.BlogBaseURL, e, settings.Timezone()) }
		sent, err := n.Comment(ctx, &entry, &comment, true)
		if err != nil {
			t.Fatalf("notify: %v", err)
		}
		if sent != 1 {
			t.Fatalf("sent %d messages, want 1 (the owner)", sent)
		}
		msgs := recorder.Messages()
		text := msgs[0].Subject + "\n" + msgs[0].Body
		if strings.Contains(text, evilHost) {
			t.Errorf("the notification carries the forged host:\n%s", text)
		}
		if !strings.Contains(text, permalink) {
			t.Errorf("the notification does not link to %s:\n%s", permalink, text)
		}
	})
}
