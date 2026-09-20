package i18n

import (
	"testing"
	"time"
)

// TestFP_R05_BundlesAndFallbackToEnUS pins the bundle set, the locale
// fallback and the per-key fallback (PLAN §9 R05).
func TestFP_R05_BundlesAndFallbackToEnUS(t *testing.T) {
	if err := LoadError(); err != nil {
		t.Fatalf("LoadError: %v", err)
	}

	// Every locale BlogCFC ships has a bundle of its own.
	for _, locale := range []string{"en_US", "de_DE", "de_AT", "de_CH"} {
		if b := New(locale); b.Locale() != locale {
			t.Errorf("New(%q).Locale() = %q, want %q", locale, b.Locale(), locale)
		}
		if !Known(locale) {
			t.Errorf("Known(%q) = false", locale)
		}
	}

	// German strings come from the German bundles.
	for _, tc := range []struct{ locale, key, want string }{
		{"de_DE", "comments", "Kommentare"},
		{"de_DE", "noentries", "Es gibt zurzeit keine Blog-Eintr&auml;ge."},
		{"de_AT", "calendar", "Kalender"},
		{"de_CH", "archivesbymonth", "Archive nach Monat"},
		{"en_US", "comments", "Comments"},
	} {
		if got := New(tc.locale).T(tc.key); got != tc.want {
			t.Errorf("New(%q).T(%q) = %q, want %q", tc.locale, tc.key, got, tc.want)
		}
	}

	// An unknown locale is en_US, however it is spelled.
	for _, locale := range []string{"fr_FR", "de", "", "xx_YY", "nonsense"} {
		b := New(locale)
		if b.Locale() != DefaultLocale {
			t.Errorf("New(%q).Locale() = %q, want %q", locale, b.Locale(), DefaultLocale)
		}
		if got, want := b.T("comments"), "Comments"; got != want {
			t.Errorf("New(%q).T(comments) = %q, want %q", locale, got, want)
		}
	}

	// Spelling of a known locale: case and the BCP-47 dash both reach it.
	for _, locale := range []string{"de-DE", "DE_de", " de_DE "} {
		if got := New(locale).Locale(); got != "de_DE" {
			t.Errorf("New(%q).Locale() = %q, want de_DE", locale, got)
		}
	}

	// The one key the German bundles are missing falls back to en_US's
	// value rather than to the key name.
	for _, locale := range []string{"de_DE", "de_AT", "de_CH"} {
		b := New(locale)
		if got, want := b.T("latestfromcfbloggers"), "Latest from CFBloggers"; got != want {
			t.Errorf("New(%q).T(latestfromcfbloggers) = %q, want %q", locale, got, want)
		}
		if !b.Has("latestfromcfbloggers") {
			t.Errorf("New(%q).Has(latestfromcfbloggers) = false", locale)
		}
		// A key no bundle has is still the key itself.
		if got := b.T("no.such.key"); got != "no.such.key" {
			t.Errorf("New(%q).T(unknown) = %q, want the key back", locale, got)
		}
		if b.Has("no.such.key") {
			t.Errorf("New(%q).Has(unknown) = true", locale)
		}
	}
}

// TestFP_R06_LocalizedDatesAndWeekStart pins the names, the formats and
// the calendar's week start (PLAN §9 R06), against what BlogCFC's JVM
// answered through org/hastings/locale/utils.cfc.
func TestFP_R06_LocalizedDatesAndWeekStart(t *testing.T) {
	// The entry footer's example date, in the blog's zone.
	posted := time.Date(2026, time.September, 20, 0, 3, 0, 0, time.UTC)

	t.Run("week start", func(t *testing.T) {
		for locale, want := range map[string]time.Weekday{
			"en_US": time.Sunday,
			"de_DE": time.Monday,
			"de_AT": time.Monday,
			"de_CH": time.Monday,
			"fr_FR": time.Sunday, // unknown: en_US
		} {
			if got := FirstDayOfWeek(locale); got != want {
				t.Errorf("FirstDayOfWeek(%q) = %v, want %v", locale, got, want)
			}
		}
	})

	t.Run("month and day names", func(t *testing.T) {
		for _, tc := range []struct {
			locale string
			month  time.Month
			long   string
			short  string
		}{
			{"en_US", time.September, "September", "Sep"},
			{"en_US", time.March, "March", "Mar"},
			{"de_DE", time.March, "März", "Mrz"},
			{"de_DE", time.January, "Januar", "Jan"},
			// The JRE's de_AT called January Jänner; de_CH did not.
			{"de_AT", time.January, "Jänner", "Jän"},
			{"de_CH", time.January, "Januar", "Jan"},
			{"de_AT", time.December, "Dezember", "Dez"},
		} {
			if got := MonthName(tc.locale, tc.month); got != tc.long {
				t.Errorf("MonthName(%q, %v) = %q, want %q", tc.locale, tc.month, got, tc.long)
			}
			if got := ShortMonth(tc.locale, tc.month); got != tc.short {
				t.Errorf("ShortMonth(%q, %v) = %q, want %q", tc.locale, tc.month, got, tc.short)
			}
		}
		for _, tc := range []struct {
			locale string
			day    time.Weekday
			long   string
			short  string
		}{
			{"en_US", time.Sunday, "Sunday", "Sun"},
			{"en_US", time.Saturday, "Saturday", "Sat"},
			{"de_DE", time.Sunday, "Sonntag", "So"},
			{"de_DE", time.Wednesday, "Mittwoch", "Mi"},
			{"de_CH", time.Saturday, "Samstag", "Sa"},
		} {
			if got := DayName(tc.locale, tc.day); got != tc.long {
				t.Errorf("DayName(%q, %v) = %q, want %q", tc.locale, tc.day, got, tc.long)
			}
			if got := ShortDay(tc.locale, tc.day); got != tc.short {
				t.Errorf("ShortDay(%q, %v) = %q, want %q", tc.locale, tc.day, got, tc.short)
			}
		}
		// An out-of-range value is empty, never a panic.
		if got := MonthName("en_US", time.Month(13)); got != "" {
			t.Errorf("MonthName(13) = %q, want empty", got)
		}
		if got := DayName("en_US", time.Weekday(9)); got != "" {
			t.Errorf("DayName(9) = %q, want empty", got)
		}
	})

	t.Run("formats", func(t *testing.T) {
		for _, tc := range []struct {
			locale string
			style  DateStyle
			want   string
		}{
			// The entry footer: "This entry was posted on ... at ...".
			{"en_US", StylePosted, "September 20, 2026 at 12:03 AM"},
			{"de_DE", StylePosted, "20. September 2026 um 00:03"},
			{"de_AT", StylePosted, "20. September 2026 um 00:03"},
			// The monthly archives pod and the calendar's title.
			{"en_US", StyleMonthYear, "September 2026"},
			{"de_DE", StyleMonthYear, "September 2026"},
			// Java's LONG and SHORT dates and SHORT time.
			{"en_US", StyleLong, "September 20, 2026"},
			{"de_DE", StyleLong, "20. September 2026"},
			{"en_US", StyleShort, "9/20/26"},
			{"de_DE", StyleShort, "20.09.26"},
			{"en_US", StyleTime, "12:03 AM"},
			{"de_CH", StyleTime, "00:03"},
			// An unknown style is the long date; an unknown locale is en_US.
			{"en_US", DateStyle("nope"), "September 20, 2026"},
			{"fr_FR", StylePosted, "September 20, 2026 at 12:03 AM"},
		} {
			if got := FormatDate(tc.locale, posted, tc.style); got != tc.want {
				t.Errorf("FormatDate(%q, %v) = %q, want %q", tc.locale, tc.style, got, tc.want)
			}
		}
		// An afternoon time, where the 12-hour and 24-hour clocks part.
		afternoon := time.Date(2026, time.January, 5, 14, 30, 0, 0, time.UTC)
		if got, want := FormatDate("en_US", afternoon, StyleTime), "2:30 PM"; got != want {
			t.Errorf("FormatDate(en_US, 14:30) = %q, want %q", got, want)
		}
		if got, want := FormatDate("de_AT", afternoon, StyleLong), "5. Jänner 2026"; got != want {
			t.Errorf("FormatDate(de_AT, long) = %q, want %q", got, want)
		}
	})
}
