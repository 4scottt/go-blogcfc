package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/4scottt/go-blogcfc/internal/store"
)

// TestFP_P24_DownloadLogsAndRedirectsAndServesSafely is P24: the
// enclosure download is logged - entry, address, referrer, agent, the
// online flag - and then redirected to the file, which is served out of
// DATA_DIR and only out of DATA_DIR (PLAN §9 P24, §8, download.cfm).
func TestFP_P24_DownloadLogsAndRedirectsAndServesSafely(t *testing.T) {
	s := newExtraSite(t)
	s.author("ray", "Raymond Camden")
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	// BlogCFC stored the enclosure as a path and served its last segment.
	e := s.entry(store.Entry{Title: "Podcast", Alias: "podcast", Body: "<p>Listen.</p>",
		Enclosure: "/var/www/enclosures/podcast.mp3", MimeType: "audio/mpeg",
		Posted: now.Add(-time.Hour), Username: "ray", Released: true})
	s.writeDataFile("enclosures/podcast.mp3", "MP3 BYTES")
	// One directory up from the enclosures tree: nothing may reach it.
	s.writeDataFile("secret.txt", "SECRET")

	req := httptest.NewRequest(http.MethodGet, "/download/"+e.ID+"/podcast.mp3?online=1", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.1")
	req.Header.Set("Referer", "https://news.example/story")
	req.Header.Set("User-Agent", "oldbox-walk/1.0")
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("the download answered %d, want 302", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != testBase+"/enclosures/podcast.mp3" {
		t.Errorf("Location = %q", got)
	}

	logged, err := s.store.ListDownloads(ctx, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("ListDownloads: %v", err)
	}
	if len(logged) != 1 {
		t.Fatalf("logged %d downloads, want one", len(logged))
	}
	d := logged[0]
	if d.EntryID != e.ID || d.Enclosure != "podcast.mp3" || !d.Online {
		t.Errorf("the row is %+v", d)
	}
	if d.IP != "203.0.113.9" {
		t.Errorf("IP = %q, want the first forwarded hop", d.IP)
	}
	if d.Referrer != "https://news.example/story" || d.UserAgent != "oldbox-walk/1.0" {
		t.Errorf("referrer/agent = %q / %q", d.Referrer, d.UserAgent)
	}

	// Without ?online the fetch is a download, not a play.
	if rec := s.get("/download/" + e.ID + "/podcast.mp3"); rec.Code != http.StatusFound {
		t.Fatalf("the second download answered %d", rec.Code)
	}
	logged, err = s.store.ListDownloads(ctx, time.Time{}, time.Time{})
	if err != nil || len(logged) != 2 {
		t.Fatalf("logged %d downloads (%v), want two", len(logged), err)
	}
	var online, offline int
	for _, d := range logged {
		if d.Online {
			online++
		} else {
			offline++
		}
	}
	if online != 1 || offline != 1 {
		t.Errorf("online/offline = %d/%d, want one of each", online, offline)
	}

	// A file that is not this entry's enclosure, and an entry that does
	// not exist, are both 404 and neither is logged.
	for _, path := range []string{
		"/download/" + e.ID + "/other.mp3",
		"/download/99999999-9999-4999-8999-999999999999/podcast.mp3",
	} {
		if rec := s.get(path); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, rec.Code)
		}
	}
	if logged, _ := s.store.ListDownloads(ctx, time.Time{}, time.Time{}); len(logged) != 2 {
		t.Errorf("a refused download was still logged: %d rows", len(logged))
	}

	// The file the redirect points at.
	got := s.get("/enclosures/podcast.mp3")
	if got.Code != http.StatusOK || got.Body.String() != "MP3 BYTES" {
		t.Errorf("the enclosure answered %d %q", got.Code, got.Body.String())
	}

	// Path traversal, in the escaped form that reaches the handler: a
	// refusal, and never a byte from outside the tree.
	for _, path := range []string{
		"/enclosures/..%2Fsecret.txt",
		"/enclosures/%2e%2e%2fsecret.txt",
		"/images/uploads/..%2f..%2fsecret.txt",
		"/images/uploads/a%2f..%2f..%2fsecret.txt",
		`/enclosures/..%5Csecret.txt`,
	} {
		rec := s.get(path)
		if rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 400 or 404", path, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "SECRET") {
			t.Fatalf("GET %s served a file from outside DATA_DIR", path)
		}
	}

	// A directory is a 404, never a listing.
	s.writeDataFile("images/uploads-sample/a.jpg", "JPEG")
	for _, path := range []string{"/images/uploads/uploads-sample", "/images/uploads/uploads-sample/", "/enclosures/"} {
		rec := s.get(path)
		if rec.Code == http.StatusOK {
			t.Errorf("GET %s = 200, want no directory listing:\n%s", path, rec.Body.String())
		}
	}
}
