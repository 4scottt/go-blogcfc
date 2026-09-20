package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/4scottt/go-blogcfc/internal/store"
	"github.com/4scottt/go-blogcfc/internal/testdb"
)

// TestDownloadLogAndDateRange: the enclosure log and the report that
// reads it back.
func TestDownloadLogAndDateRange(t *testing.T) {
	st := testdb.New(t)
	ctx := context.Background()

	entry := mustCreateEntry(t, st, &store.Entry{Title: "Podcast", Alias: "podcast", Body: "b",
		Posted: time.Now().UTC().Add(-time.Hour), Username: "admin", Released: true,
		Enclosure: "show1.mp3", MimeType: "audio/mpeg"})

	jan := time.Date(2026, time.January, 10, 12, 0, 0, 0, time.UTC)
	feb := time.Date(2026, time.February, 10, 12, 0, 0, 0, time.UTC)
	mar := time.Date(2026, time.March, 10, 12, 0, 0, 0, time.UTC)

	for _, d := range []*store.Download{
		{EntryID: entry.ID, Enclosure: "show1.mp3", IP: "10.0.0.1", Referrer: "http://blog.example/",
			UserAgent: "curl/8", DownloadedAt: jan},
		{EntryID: entry.ID, Enclosure: "show1.mp3", IP: "10.0.0.2", DownloadedAt: feb, Online: true},
		{EntryID: entry.ID, Enclosure: "show1.mp3", IP: "10.0.0.3", DownloadedAt: mar},
	} {
		if err := st.LogDownload(ctx, d); err != nil {
			t.Fatalf("LogDownload: %v", err)
		}
		if d.ID == "" {
			t.Fatal("LogDownload did not fill the id")
		}
	}

	// A download with neither an id nor a time gets both.
	loose := &store.Download{EntryID: entry.ID, Enclosure: "show1.mp3"}
	if err := st.LogDownload(ctx, loose); err != nil {
		t.Fatalf("LogDownload: %v", err)
	}
	if loose.DownloadedAt.IsZero() {
		t.Error("LogDownload left DownloadedAt zero")
	}

	inRange, err := st.ListDownloads(ctx, jan.Add(-time.Hour), mar.Add(-time.Hour))
	if err != nil {
		t.Fatalf("ListDownloads: %v", err)
	}
	if len(inRange) != 2 {
		t.Fatalf("range = %d rows, want the January and February fetches", len(inRange))
	}
	if !inRange[0].DownloadedAt.Equal(feb) {
		t.Errorf("first row is %v, want the newest (%v)", inRange[0].DownloadedAt, feb)
	}
	if inRange[0].IP != "10.0.0.2" || !inRange[0].Online {
		t.Errorf("the online play round-tripped as %+v", inRange[0])
	}
	if inRange[1].Referrer != "http://blog.example/" || inRange[1].UserAgent != "curl/8" {
		t.Errorf("the January fetch round-tripped as %+v", inRange[1])
	}

	all, err := st.ListDownloads(ctx, time.Time{}, time.Time{})
	if err != nil || len(all) != 4 {
		t.Fatalf("unbounded list = %d rows (%v), want 4", len(all), err)
	}
	since, err := st.ListDownloads(ctx, feb, time.Time{})
	if err != nil || len(since) != 3 {
		t.Fatalf("open-ended range = %d rows (%v), want 3", len(since), err)
	}
}
