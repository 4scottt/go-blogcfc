package render

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

// golden compares got against testdata/<name>, or rewrites it under
// -update. The highlighted HTML is too long and too much Chroma's
// business to spell out in a test; what the pipeline does with it is
// asserted in the test body, and the golden catches the day Chroma
// changes its markup.
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s (run `go test ./internal/render/... -update`): %v", path, err)
	}
	if got != string(want) {
		t.Errorf("%s does not match:\n got: %s\nwant: %s", path, got, want)
	}
}

const goSource = "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hi & bye\")\n}"

// TestFP_R01_CodeBlocksHighlightedAndEscapedInPrint pins the code block
// pass (PLAN §9 R01): `<code>` highlighted into div.code on the site,
// escaped into pre.codePrint in the print view.
func TestFP_R01_CodeBlocksHighlightedAndEscapedInPrint(t *testing.T) {
	body := "Before.\n\n<code>" + goSource + "</code>\n\nAfter."

	t.Run("site", func(t *testing.T) {
		got := string(Entry(body, Options{}))
		golden(t, "code_site.html", got)
		if !strings.Contains(got, `<div class="code">`) {
			t.Error("no div.code around the block")
		}
		// The body's own <code> tags are gone. (Chroma's `<pre>` wrapper
		// has a <code> of its own, which is why this looks for the tag
		// with the source behind it.)
		if strings.Contains(got, "<code>package main") {
			t.Error("the body's <code> tags survived")
		}
		if n := strings.Count(got, `<div class="code">`); n != 1 {
			t.Errorf("%d code blocks rendered, want 1", n)
		}
		// Chroma's classes, not inline styles: CSS() carries the colours.
		if !strings.Contains(got, `class="chroma"`) {
			t.Error("no chroma wrapper")
		}
		if strings.Contains(got, "style=") {
			t.Error("inline styles: the formatter should be writing classes")
		}
		// The code's own & and " are escaped, and its blank line did not
		// become a paragraph break inside the <pre>.
		if !strings.Contains(got, "&amp;") {
			t.Error("the ampersand inside the code was not escaped")
		}
		block := got[strings.Index(got, `<div class="code">`):]
		block = block[:strings.Index(block, "</div>")]
		if strings.Contains(block, "</p><p>") {
			t.Errorf("the paragraph formatter reached inside the code block: %s", block)
		}
		// The text around it still wraps.
		if !strings.HasPrefix(got, "<p>Before.</p><p>") || !strings.HasSuffix(got, "</p><p>After.</p>") {
			t.Errorf("the surrounding text lost its paragraphs: %s", got)
		}
	})

	t.Run("print", func(t *testing.T) {
		got := string(Print(body, Options{}))
		golden(t, "code_print.html", got)
		if !strings.Contains(got, `<pre class="codePrint">`) {
			t.Error("no pre.codePrint in the print view")
		}
		if strings.Contains(got, `<div class="code">`) || strings.Contains(got, "chroma") {
			t.Error("the print view highlighted the block")
		}
		if !strings.Contains(got, "fmt.Println(&#34;hi &amp; bye&#34;)") {
			t.Errorf("the code was not escaped: %s", got)
		}
	})

	t.Run("two blocks, mixed case tags", func(t *testing.T) {
		got := string(Entry("a<CODE>one</Code>b<code>two</code>c", Options{IgnoreParagraphs: true}))
		if n := strings.Count(got, `<div class="code">`); n != 2 {
			t.Errorf("%d code blocks rendered, want 2: %s", n, got)
		}
		for _, want := range []string{"a<div", "b<div", "c"} {
			if !strings.Contains(got, want) {
				t.Errorf("lost the text around the blocks: %s", got)
			}
		}
	})

	t.Run("an empty block disappears", func(t *testing.T) {
		got := string(Entry("a<code>   </code>b", Options{IgnoreParagraphs: true}))
		if got != "ab" {
			t.Errorf("Entry = %q, want %q", got, "ab")
		}
	})

	t.Run("an unclosed block is left alone", func(t *testing.T) {
		in := "a<code>never closed"
		got := string(Entry(in, Options{IgnoreParagraphs: true}))
		if got != in {
			t.Errorf("Entry = %q, want it untouched (%q)", got, in)
		}
	})

	t.Run("CSS", func(t *testing.T) {
		css := CSS()
		if !strings.Contains(css, ".chroma .k") {
			t.Error("CSS() has no rules for Chroma's classes")
		}
		// The site's code box keeps its own background.
		if !strings.Contains(css, ".code .chroma { background-color: transparent; }") {
			t.Error("CSS() does not clear Chroma's own background")
		}
		// Nothing unscoped that could collide with the site's stylesheet.
		if strings.Contains(css, " .bg {") {
			t.Error("CSS() still writes the bare .bg rule")
		}
	})
}

// TestFP_R02_ImageAndAudioEnclosures pins the enclosure decorations
// (PLAN §9 R02).
func TestFP_R02_ImageAndAudioEnclosures(t *testing.T) {
	const body = "Listen to this."
	cases := []struct {
		name string
		opts Options
		want string
	}{
		{
			"an image goes on top",
			Options{Enclosure: "cat.png", EnclosureURL: "/enclosures/cat.png", MimeType: "image/png"},
			`<div class="autoImage"><img src="/enclosures/cat.png" alt=""></div>` + body,
		},
		{
			"an mp3 goes underneath, where the Flash player was",
			Options{Enclosure: "show.mp3", EnclosureURL: "/enclosures/show.mp3", MimeType: "audio/mpeg"},
			body + `<audio controls src="/enclosures/show.mp3"></audio>`,
		},
		{
			"no mimetype falls back to the extension, as BlogCFC did",
			Options{Enclosure: "holiday.JPG", EnclosureURL: "/enclosures/holiday.JPG"},
			`<div class="autoImage"><img src="/enclosures/holiday.JPG" alt=""></div>` + body,
		},
		{
			"an mp3 by extension",
			Options{Enclosure: "ep1.mp3", EnclosureURL: "/enclosures/ep1.mp3"},
			body + `<audio controls src="/enclosures/ep1.mp3"></audio>`,
		},
		{
			"another type gets nothing",
			Options{Enclosure: "paper.pdf", EnclosureURL: "/enclosures/paper.pdf", MimeType: "application/pdf"},
			body,
		},
		{
			"no URL, no decoration",
			Options{Enclosure: "cat.png", MimeType: "image/png"},
			body,
		},
		{
			"the URL is escaped, the alt text is empty",
			Options{EnclosureURL: `/enclosures/a"b&c.png`, MimeType: "image/png"},
			`<div class="autoImage"><img src="/enclosures/a&#34;b&amp;c.png" alt=""></div>` + body,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.opts.IgnoreParagraphs = true
			if got := string(Entry(body, tc.opts)); got != tc.want {
				t.Errorf("Entry =\n %s\nwant\n %s", got, tc.want)
			}
		})
	}

	t.Run("the decoration is inside the paragraph wrapping, as in the as-is", func(t *testing.T) {
		got := string(Entry(body, Options{EnclosureURL: "/e/cat.png", MimeType: "image/gif"}))
		want := `<p><div class="autoImage"><img src="/e/cat.png" alt=""></div>Listen to this.</p>`
		if got != want {
			t.Errorf("Entry = %s, want %s", got, want)
		}
	})
}

// TestFP_R03_ParagraphWrappingUnlessIgnored pins XHTMLParagraphFormat
// (PLAN §9 R03).
func TestFP_R03_ParagraphWrappingUnlessIgnored(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		ignore bool
		want   string
	}{
		{"a blank line starts a paragraph", "one\n\ntwo", false, "<p>one</p><p>two</p>"},
		{"CRLF, which is what the editor posts", "one\r\n\r\ntwo", false, "<p>one</p><p>two</p>"},
		{"a single newline is left alone: no <br>", "one\ntwo", false, "<p>one\ntwo</p>"},
		{"two blank lines leave an empty paragraph, as the CFML does", "a\n\n\n\nb", false, "<p>a</p><p></p><p>b</p>"},
		{"three newlines split once and keep the odd one", "a\n\n\nb", false, "<p>a</p><p>\nb</p>"},
		{"an empty body is an empty paragraph", "", false, "<p></p>"},
		{"a body with its own <p> is wrapped anyway", "<p>x</p>\n\n<p>y</p>", false, "<p><p>x</p></p><p><p>y</p></p>"},
		{"markup is never escaped: the body is trusted", `<b>bold</b> <a href="/x">link</a>`, false, `<p><b>bold</b> <a href="/x">link</a></p>`},
		{"IgnoreParagraphs skips the pass entirely", "one\n\ntwo", true, "one\n\ntwo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(Entry(tc.in, Options{IgnoreParagraphs: tc.ignore})); got != tc.want {
				t.Errorf("Entry(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}

	t.Run("the print view wraps too", func(t *testing.T) {
		if got, want := string(Print("one\n\ntwo", Options{})), "<p>one</p><p>two</p>"; got != want {
			t.Errorf("Print = %q, want %q", got, want)
		}
	})
}

// TestTextblocksAreSubstitutedByLabel pins the `<textblock label="x">`
// tag BlogCFC's renderEntry expanded from tblblogtextblocks.
func TestTextblocksAreSubstitutedByLabel(t *testing.T) {
	blocks := map[string]string{
		"disclaimer": "<em>My own opinion.</em>",
		"two words":  "second",
	}
	lookup := func(label string) (string, bool) {
		v, ok := blocks[label]
		return v, ok
	}

	cases := []struct {
		name   string
		in     string
		lookup func(string) (string, bool)
		want   string
	}{
		{"a known label", `a<textblock label="disclaimer">b`, lookup, "a<em>My own opinion.</em>b"},
		{"the label may hold spaces", `<textblock label="two words">`, lookup, "second"},
		{"BlogCFC's loose spacing", `<textblock   label = "disclaimer" >x`, lookup, `<textblock   label = "disclaimer" >x`},
		{"spacing the CFML's regex allowed", `<textblock   label = "disclaimer">x`, lookup, "<em>My own opinion.</em>x"},
		{"case is ignored on the tag, kept on the label", `<TEXTBLOCK LABEL="disclaimer">`, lookup, "<em>My own opinion.</em>"},
		{"an unknown label is left in the page", `a<textblock label="nope">b`, lookup, `a<textblock label="nope">b`},
		{"two tags, one of them unknown", `<textblock label="disclaimer"><textblock label="nope">`, lookup, `<em>My own opinion.</em><textblock label="nope">`},
		{"no resolver leaves every tag alone", `<textblock label="disclaimer">`, nil, `<textblock label="disclaimer">`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := string(Entry(tc.in, Options{IgnoreParagraphs: true, Textblocks: tc.lookup}))
			if got != tc.want {
				t.Errorf("Entry(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}

	t.Run("a tag inside a code block is code, not a tag", func(t *testing.T) {
		got := string(Entry(`<code><textblock label="disclaimer"></code>`, Options{IgnoreParagraphs: true, Textblocks: lookup}))
		if strings.Contains(got, "My own opinion") {
			t.Errorf("the textblock inside the code block was expanded: %s", got)
		}
	})
}
