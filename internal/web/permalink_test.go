package web

import (
	"testing"
	"time"

	"github.com/4scottt/go-blogcfc/internal/store"
)

// TestFP_P05_PermalinkShapeMatchesBlogCFC pins makeLink, makeCategoryLink
// and makeUserLink (PLAN §9 P05): the date segments come from the blog's
// zone, carry no zero padding, and a row without an alias falls back to
// the id form. Nothing is ever built from a request host.
func TestFP_P05_PermalinkShapeMatchesBlogCFC(t *testing.T) {
	la, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("zone: %v", err)
	}

	t.Run("alias uses the date in the blog's zone", func(t *testing.T) {
		cases := []struct {
			name   string
			posted time.Time
			loc    *time.Location
			want   string
		}{
			{
				// The zone pushes the entry back into the previous month.
				// (2026 is not a leap year: the day before 1 March is the
				// 28th. The 29th case below is the leap year 2024.)
				name:   "the zone moves the entry to the previous day and month",
				posted: time.Date(2026, 3, 1, 2, 0, 0, 0, time.UTC),
				loc:    la,
				want:   testBase + "/2026/2/28/hello-world",
			},
			{
				name:   "leap day",
				posted: time.Date(2024, 3, 1, 2, 0, 0, 0, time.UTC),
				loc:    la,
				want:   testBase + "/2024/2/29/hello-world",
			},
			{
				name:   "no zero padding on month or day",
				posted: time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC),
				loc:    time.UTC,
				want:   testBase + "/2026/1/5/hello-world",
			},
			{
				name:   "a nil zone means UTC",
				posted: time.Date(2026, 3, 1, 2, 0, 0, 0, time.UTC),
				loc:    nil,
				want:   testBase + "/2026/3/1/hello-world",
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				e := store.Entry{ID: "e1", Alias: "hello-world", Posted: tc.posted}
				if got := EntryURL(testBase, e, tc.loc); got != tc.want {
					t.Errorf("EntryURL = %q, want %q", got, tc.want)
				}
			})
		}
	})

	t.Run("no alias falls back to the id form", func(t *testing.T) {
		e := store.Entry{ID: "8a3e4b2c-0000-4000-8000-000000000001", Posted: utc(2026, 3, 1, 2, 0)}
		want := testBase + "/?mode=entry&entry=8a3e4b2c-0000-4000-8000-000000000001"
		if got := EntryURL(testBase, e, la); got != want {
			t.Errorf("EntryURL = %q, want %q", got, want)
		}
	})

	t.Run("a trailing slash on the base is dropped", func(t *testing.T) {
		e := store.Entry{ID: "e1", Alias: "a", Posted: utc(2026, 3, 2, 0, 0)}
		if got := EntryURL(testBase+"/", e, time.UTC); got != testBase+"/2026/3/2/a" {
			t.Errorf("EntryURL = %q", got)
		}
	})

	t.Run("category links", func(t *testing.T) {
		withAlias := store.Category{ID: "c1", Name: "ColdFusion", Alias: "coldfusion"}
		if got, want := CategoryURL(testBase, withAlias), testBase+"/coldfusion"; got != want {
			t.Errorf("CategoryURL = %q, want %q", got, want)
		}
		noAlias := store.Category{ID: "c2", Name: "Odds & Ends"}
		if got, want := CategoryURL(testBase, noAlias), testBase+"/?mode=cat&catid=c2"; got != want {
			t.Errorf("CategoryURL = %q, want %q", got, want)
		}
	})

	t.Run("user links", func(t *testing.T) {
		if got, want := UserURL(testBase, "ray"), testBase+"/postedby/ray"; got != want {
			t.Errorf("UserURL = %q, want %q", got, want)
		}
	})
}
