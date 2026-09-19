// Package render turns entry content into HTML. For now it holds only the
// slugifier; the code highlighter, the paragraph formatter and the
// enclosure decorations (PLAN §9 R01-R03) arrive with M2.
package render

import (
	"regexp"
	"strings"
)

// entityPattern is BlogCFC's `&[^;]+;`: an HTML entity, kept short by the
// fact that the character class cannot cross the closing semicolon.
var entityPattern = regexp.MustCompile(`&[^;]+;`)

// MakeTitle is BlogCFC's makeTitle (org/camden/blog/blog.cfc), the
// slugifier behind entry, page and category aliases (PLAN §9 R04). The
// steps and their order are the CFML's:
//
//  1. the literal `&amp;` becomes "and" (the CFML replaces the entity, not
//     a bare ampersand: a bare `&` is dropped by step 3 instead),
//  2. HTML entities (`&[^;]+;`) are removed,
//  3. everything but `0-9a-zA-Z` and a space is removed, which also drops
//     every non-ASCII letter, as the CFML's comment intends,
//  4. spaces become hyphens.
//
// Case is kept and nothing is trimmed or collapsed, so "  Hello   World "
// slugs to "--Hello---World-", exactly as BlogCFC does.
func MakeTitle(s string) string {
	s = strings.ReplaceAll(s, "&amp;", "and")
	s = entityPattern.ReplaceAllString(s, "")
	s = stripNonAlnum(s)
	return strings.ReplaceAll(s, " ", "-")
}

// stripNonAlnum drops every rune outside [0-9a-zA-Z ]. It works on runes so
// a multi-byte character goes as one unit and never leaves a broken byte.
func stripNonAlnum(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == ' ':
			b.WriteRune(r)
		}
	}
	return b.String()
}
