package admin_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/4scottt/go-blogcfc/internal/store"
)

// TestFP_A27_DownloadsReportByDateRange covers PLAN §9 A27
// (admin/downloads.cfm): the report opens on the last thirty days and
// lists the logged fetches -- date, entry, file, address and whether it
// was played online -- for whatever range the form asks for.
func TestFP_A27_DownloadsReportByDateRange(t *testing.T) {
	h := newHarness(t)
	h.user("admin", "Admin")
	ctx := context.Background()
	now := time.Now().UTC()

	e := h.createEntry(&store.Entry{Title: "The podcast episode", Alias: "episode", Body: "b",
		Posted: now.Add(-90 * 24 * time.Hour), Username: "admin", Released: true,
		Enclosure: "show.mp3", FileSize: 10, MimeType: "audio/mpeg"})

	logs := []*store.Download{
		{EntryID: e.ID, IP: "203.0.113.9", Enclosure: "show.mp3", DownloadedAt: now.Add(-2 * time.Hour)},
		{EntryID: e.ID, IP: "203.0.113.10", Enclosure: "show.mp3", DownloadedAt: now.Add(-3 * 24 * time.Hour), Online: true},
		{EntryID: e.ID, IP: "198.51.100.4", Enclosure: "show.mp3", DownloadedAt: now.Add(-100 * 24 * time.Hour)},
	}
	for _, d := range logs {
		if err := h.store.LogDownload(ctx, d); err != nil {
			t.Fatalf("LogDownload: %v", err)
		}
	}

	h.login("admin")
	resp, body := h.get("/admin/downloads")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("downloads: status %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "<h1>Downloads</h1>") {
		t.Error("the report has no Downloads heading")
	}
	for _, want := range []string{`name="from"`, `name="to"`, `value="Show Downloads"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the date-range form has no %s", want)
		}
	}
	for _, want := range []string{"The podcast episode", "show.mp3", "203.0.113.9", "203.0.113.10"} {
		if !strings.Contains(body, want) {
			t.Errorf("the default range does not list %q", want)
		}
	}
	if strings.Contains(body, "198.51.100.4") {
		t.Error("a fetch from a hundred days ago is inside the default thirty-day range")
	}
	// The online column tells a play from a download, which the as-is
	// admitted in a comment that it never did.
	if o := order(t, body, "203.0.113.9", "203.0.113.10"); o[0] > o[1] {
		t.Error("the report is not newest first")
	}
	if !strings.Contains(body, "<th>Online</th>") {
		t.Error("the report has no Online column")
	}

	// A range of its own: only the middle fetch.
	day := func(t time.Time) string { return t.Format("2006-01-02") }
	resp, body = h.get("/admin/downloads?from=" + day(now.Add(-4*24*time.Hour)) + "&to=" + day(now.Add(-2*24*time.Hour)))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("filtered downloads: status %d", resp.StatusCode)
	}
	if !strings.Contains(body, "203.0.113.10") {
		t.Error("the chosen range does not list the fetch inside it")
	}
	for _, gone := range []string{"203.0.113.9", "198.51.100.4"} {
		if strings.Contains(body, gone) {
			t.Errorf("the chosen range lists %q, which is outside it", gone)
		}
	}

	// An empty range says so rather than showing an empty table.
	_, body = h.get("/admin/downloads?from=2001-01-01&to=2001-01-02")
	if !strings.Contains(body, "No downloads") {
		t.Error("an empty range does not say it is empty")
	}
}
