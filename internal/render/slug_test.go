package render

import "testing"

// TestFP_R04_MakeTitleSlugifier pins BlogCFC's makeTitle (PLAN §9 R04).
// Every expectation was read off the CFML in
// org/camden/blog/blog.cfc: replace("&amp;","and"), reReplace("&[^;]+;",""),
// reReplace("[^0-9a-zA-Z ]",""), replace(" ","-").
func TestFP_R04_MakeTitleSlugifier(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain words", "Hello World", "Hello-World"},
		{"case is kept", "HeLLo WoRLD", "HeLLo-WoRLD"},
		{"digits survive", "Top 10 Reasons", "Top-10-Reasons"},
		{"encoded ampersand becomes and", "Tom &amp; Jerry", "Tom-and-Jerry"},
		// The CFML replaces the entity, never a bare `&`; the bare one is
		// dropped by the alphanumeric filter, leaving the two spaces.
		{"bare ampersand is dropped, not replaced", "Tom & Jerry", "Tom--Jerry"},
		{"entities are stripped", "Caf&eacute; life", "Caf-life"},
		{"numeric entity is stripped", "A &#233; B", "A--B"},
		{"an unterminated entity is left to the filter", "A &amp B", "A-amp-B"},
		{"punctuation goes", "It's a test: really, now!", "Its-a-test-really-now"},
		{"slashes and dots go", "CF 9.0.1 / Railo", "CF-901--Railo"},
		{"an existing hyphen is not a word character", "well-known bug", "wellknown-bug"},
		{"runs of spaces become runs of hyphens", "a   b", "a---b"},
		{"leading and trailing spaces become hyphens", "  padded  ", "--padded--"},
		{"tabs and newlines are simply removed", "a\tb\nc", "abc"},
		{"non-ascii letters are removed", "Über Straße", "ber-Strae"},
		{"cjk is removed", "日本 blog", "-blog"},
		{"empty stays empty", "", ""},
		{"all punctuation collapses to nothing", "!!!", ""},
		{"a tag in the title loses its angle brackets", "<b>bold</b> title", "bboldb-title"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MakeTitle(tc.in); got != tc.want {
				t.Errorf("MakeTitle(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
