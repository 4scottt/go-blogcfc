package render

import (
	"bytes"
	"html"
	"regexp"
	"strings"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// BlogCFC found code blocks with findNoCase("<code>") and a greedy
// "(?s)(.*)(<code>)(.*)(</code>)(.*)", which processes the *last* pair
// first and loops. These two patterns walk the pairs left to right
// instead, which is the same output for every well-formed body and is
// not at the mercy of a regex that also matches across two blocks.
var (
	codeOpenPattern  = regexp.MustCompile(`(?i)<code>`)
	codeClosePattern = regexp.MustCompile(`(?i)</code>`)
)

// codeStyle is the Chroma style the stylesheet is generated from. It is
// only read by CSS(): the formatter writes classes, not inline styles.
// Chroma's github style is a light one, close to what ColdFish painted
// on BlogCFC's pale yellow code box.
var codeStyle = styles.Get("github")

// codeFormatter writes `<pre class="chroma">` with Chroma's class names,
// so one stylesheet colours every block (see CSS()).
var codeFormatter = chromahtml.New(chromahtml.WithClasses(true), chromahtml.TabWidth(4))

// CSS returns the stylesheet for the classes the code blocks carry. The
// static CSS package includes it (once, anywhere after the site's own
// rules); the `<div class="code">` box around it is the site's.
func CSS() string {
	var buf bytes.Buffer
	if err := codeFormatter.WriteCSS(&buf, codeStyle); err != nil {
		return ""
	}
	var out strings.Builder
	for _, line := range strings.Split(buf.String(), "\n") {
		// Chroma writes a bare `.bg` rule for a wrapper this package
		// never emits. A single-word class name in a shared stylesheet is
		// a collision waiting to happen, so it is dropped; every other
		// rule of Chroma's is scoped under `.chroma`.
		if strings.Contains(line, " .bg {") {
			continue
		}
		out.WriteString(line)
		out.WriteString("\n")
	}
	// The style's own page background would paint over `div.code`, the
	// pale yellow box with the blue border that BlogCFC's style.css draws
	// around a code block. The box wins.
	out.WriteString(".code .chroma { background-color: transparent; }\n")
	return out.String()
}

// expandCodeBlocks replaces every `<code>...</code>` pair (PLAN §9 R01).
// Each rendered block is parked behind a placeholder so the later passes
// - textblocks and the paragraph formatter - cannot reach inside it;
// BlogCFC got that for free because ColdFish escaped the code and turned
// its newlines into `<br />`, where Chroma keeps real newlines in a
// `<pre>`. The placeholders are resolved by restoreCodeBlocks.
func expandCodeBlocks(s string, forPrint bool) (string, []string) {
	var (
		out    strings.Builder
		blocks []string
		rest   = s
	)
	for {
		open := codeOpenPattern.FindStringIndex(rest)
		if open == nil {
			break
		}
		tail := rest[open[1]:]
		closing := codeClosePattern.FindStringIndex(tail)
		if closing == nil {
			// `<code>` with no ender: BlogCFC stopped here too, leaving
			// the rest of the body untouched.
			break
		}
		out.WriteString(rest[:open[0]])
		out.WriteString(placeholder(len(blocks)))
		blocks = append(blocks, renderCode(tail[:closing[0]], forPrint))
		rest = tail[closing[1]:]
	}
	out.WriteString(rest)
	return out.String(), blocks
}

// renderCode is the body of one block: highlighted inside `div.code`, or
// escaped inside `pre.codePrint` for the print view. An empty block
// disappears, tags and all, as BlogCFC's `len(trim(codeportion))` did.
func renderCode(code string, forPrint bool) string {
	code = strings.TrimSpace(code)
	if code == "" {
		return ""
	}
	if forPrint {
		return `<br/><pre class="codePrint">` + html.EscapeString(code) + `</pre><br/>`
	}
	return `<div class="code">` + highlight(code) + `</div>`
}

// highlight runs Chroma over the block. The language is guessed from the
// text - BlogCFC's `<code>` carries no attributes, so there is nothing
// else to go on - and plain text is the fallback, as it is when the
// lexer or the formatter fails.
func highlight(code string) string {
	lexer := lexers.Analyse(code)
	if lexer == nil {
		lexer = lexers.Fallback
	}
	iterator, err := chroma.Coalesce(lexer).Tokenise(nil, code)
	if err != nil {
		return html.EscapeString(code)
	}
	var buf bytes.Buffer
	if err := codeFormatter.Format(&buf, codeStyle, iterator); err != nil {
		return html.EscapeString(code)
	}
	return buf.String()
}
