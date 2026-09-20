package xmlrpc

import (
	"strings"
	"testing"
)

// TestFP_X10_ParseMarkupToggle covers the `parseMarkup` pair on its own
// (PLAN §9 X10): what the blog stores as a `<code>` block leaves as
// escaped text a rich-text editor will not run, and what such an editor
// posts back becomes the stored form again.
func TestFP_X10_ParseMarkupToggle(t *testing.T) {
	t.Run("escape hides a code block from the editor", func(t *testing.T) {
		got := escapeMarkup(`before <code>a < b</code> after`)
		want := `before &lt;code&gt;a &lt; b&lt;/code&gt; after`
		if got != want {
			t.Errorf("escapeMarkup = %q, want %q", got, want)
		}
		if strings.Contains(got, "<code>") {
			t.Errorf("escapeMarkup left a live <code> tag: %q", got)
		}
	})

	t.Run("unescape brings it back", func(t *testing.T) {
		got := unescapeMarkup(`before &lt;code&gt;a &lt; b&lt;/code&gt; after`)
		want := `before <code>a < b</code> after`
		if got != want {
			t.Errorf("unescapeMarkup = %q, want %q", got, want)
		}
	})

	t.Run("round trip", func(t *testing.T) {
		const stored = `intro <code>if (a < b) { "x" }</code> outro`
		if got := unescapeMarkup(escapeMarkup(stored)); got != stored {
			t.Errorf("round trip = %q, want %q", got, stored)
		}
	})

	t.Run("an ampersand survives both directions", func(t *testing.T) {
		const stored = `<code>a &amp; b</code>`
		escaped := escapeMarkup(stored)
		if !strings.Contains(escaped, "&amp;amp;") {
			t.Errorf("escapeMarkup did not escape the entity: %q", escaped)
		}
		if got := unescapeMarkup(escaped); got != stored {
			t.Errorf("round trip = %q, want %q", got, stored)
		}
	})

	t.Run("both blocks are converted", func(t *testing.T) {
		got := escapeMarkup(`<code>one</code> and <code>two</code>`)
		if strings.Contains(got, "<code>") {
			t.Errorf("a block was left live: %q", got)
		}
		if strings.Count(got, "&lt;code&gt;") != 2 {
			t.Errorf("escapeMarkup = %q, want two escaped blocks", got)
		}
	})

	t.Run("an empty block is dropped, as the as-is dropped it", func(t *testing.T) {
		if got := escapeMarkup(`a<code>   </code>b`); got != "ab" {
			t.Errorf("escapeMarkup = %q, want %q", got, "ab")
		}
	})

	t.Run("already-escaped code survives as itself", func(t *testing.T) {
		const posted = `&lt;code&gt;x&lt;/code&gt;`
		escaped := escapeMarkup(posted)
		if !strings.Contains(escaped, "&lt;[code]&gt;") {
			t.Errorf("escapeMarkup = %q, want the bracketed form", escaped)
		}
		if got := unescapeMarkup(escaped); got != posted {
			t.Errorf("round trip = %q, want %q", got, posted)
		}
	})

	t.Run("the more tag becomes visible markup and back", func(t *testing.T) {
		escaped := escapeMarkup("body" + moreTag + "rest")
		if escaped != `body<p>&lt;more/&gt;</p>rest` {
			t.Errorf("escapeMarkup = %q", escaped)
		}
		if got := unescapeMarkup(escaped); got != "body"+moreTag+"rest" {
			t.Errorf("unescapeMarkup = %q", got)
		}
	})

	t.Run("html a rich editor wrapped a code block in is flattened", func(t *testing.T) {
		got := unescapeMarkup(`&lt;code&gt;first<br/>second&lt;/code&gt;`)
		if !strings.HasPrefix(got, "<code>") || !strings.HasSuffix(got, "</code>") {
			t.Fatalf("unescapeMarkup = %q, want a <code> block", got)
		}
		if !strings.Contains(got, "first\r\nsecond") {
			t.Errorf("the <br/> did not become a line break: %q", got)
		}
	})

	t.Run("the toggle itself", func(t *testing.T) {
		for raw, want := range map[string]bool{
			"":      false,
			"false": false,
			"no":    false,
			"true":  true,
			"TRUE":  true,
			"yes":   true,
			"1":     true,
		} {
			if got := cfBool(raw); got != want {
				t.Errorf("cfBool(%q) = %v, want %v", raw, got, want)
			}
		}
	})
}
