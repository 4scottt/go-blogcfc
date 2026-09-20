package i18n

import "testing"

func TestParseReadsTheJavaPropertiesFormat(t *testing.T) {
	src := "" +
		"# a comment\n" +
		"! another comment\n" +
		"\n" +
		"plain = Hello\n" +
		"  indented=Indented\n" +
		"colon:Colon\n" +
		"spaced Value with spaces\n" +
		"empty =\n" +
		"escapedcolon = Trackback URL for this entry\\:\n" +
		"unicode = Caf\\u00e9 \\u65e5\\u672c\n" +
		"tabbed = one\\ttwo\n" +
		"continued = first \\\n" +
		"  second\n" +
		"dupe = one\n" +
		"dupe = two\n"

	got := Parse(src)
	want := map[string]string{
		"plain":        "Hello",
		"indented":     "Indented",
		"colon":        "Colon",
		"spaced":       "Value with spaces",
		"empty":        "",
		"escapedcolon": "Trackback URL for this entry:",
		"unicode":      "Café 日本",
		"tabbed":       "one\ttwo",
		"continued":    "first second",
		"dupe":         "two",
	}
	if len(got) != len(want) {
		t.Errorf("parsed %d keys, want %d: %v", len(got), len(want), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("Parse()[%q] = %q, want %q", k, got[k], v)
		}
	}
}

func TestBundleLoadsBlogCFCStrings(t *testing.T) {
	if err := LoadError(); err != nil {
		t.Fatalf("LoadError: %v", err)
	}
	b := New("en_US")
	if b.Locale() != "en_US" {
		t.Errorf("Locale() = %q, want en_US", b.Locale())
	}
	// Strings the public pages use (PLAN §9 P16, index.cfm).
	for key, want := range map[string]string{
		"noentries":            "There are no blog entries available.",
		"noentriesforcriteria": "There are no blog entries available that match your criteria.",
		"moreentries":          "More Entries",
		"preventries":          "Previous Entries",
		"sorry":                "Sorry",
		"more":                 "More",
		"comments":             "Comments",
		// The as-is file escapes the colon; a real properties reader
		// resolves it, where BlogCFC's own reader leaves the backslash in.
		"trackbackurl": "Trackback URL for this entry:",
	} {
		if got := b.T(key); got != want {
			t.Errorf("T(%q) = %q, want %q", key, got, want)
		}
	}
}

func TestBundleUnknownKeyReturnsTheKey(t *testing.T) {
	b := New("de_DE") // a real bundle, with en_US behind it
	if got := b.T("no.such.key"); got != "no.such.key" {
		t.Errorf("T(unknown) = %q, want the key back", got)
	}
	if b.Has("no.such.key") {
		t.Error("Has(unknown) = true")
	}
	if !b.Has("noentries") {
		t.Error("Has(noentries) = false")
	}
}

func TestBundleSubstitutesPlaceholders(t *testing.T) {
	values := Parse("greeting = Hello {1}, you have {2} messages\nliteral = 100{}% and {9}\n")
	b := &Bundle{locale: "en_US", values: values}

	if got, want := b.T("greeting", "Ray", 3), "Hello Ray, you have 3 messages"; got != want {
		t.Errorf("T = %q, want %q", got, want)
	}
	// Placeholders without an argument stay visible rather than vanishing.
	if got, want := b.T("greeting", "Ray"), "Hello Ray, you have {2} messages"; got != want {
		t.Errorf("T with too few args = %q, want %q", got, want)
	}
	// No arguments at all leaves the string untouched.
	if got, want := b.T("greeting"), "Hello {1}, you have {2} messages"; got != want {
		t.Errorf("T with no args = %q, want %q", got, want)
	}
	if got, want := b.T("literal", "x"), "100{}% and {9}"; got != want {
		t.Errorf("T with non-placeholder braces = %q, want %q", got, want)
	}
}
