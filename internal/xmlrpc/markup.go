package xmlrpc

import (
	"regexp"
	"strings"
)

// The `parseMarkup` toggle (X10). BlogCFC's xmlrpc.cfm takes it as a URL
// parameter -- `POST /xmlrpc?parseMarkup=true` -- and it exists because a
// rich-text client (Windows Live Writer) and a blog that stores code in
// `<code>` blocks disagree about what is markup and what is text.
//
//   - on the way out (getRecentPosts, getPost) escapeMarkup turns a
//     stored `<code>…</code>` block into `&lt;code&gt;` with its contents
//     HTML-escaped, so the editor shows the code rather than running it;
//   - on the way in (newPost, editPost) unescapeMarkup turns it back.
//
// The pair is the as-is's, including its greedy last-block-first loop. Two
// things are not ported literally: RE2 has no lookahead, so the CFML's
// `[^\s]*(?=&gt;)` is a lazy `[^\s]*?` before the same `&gt;`, which
// matches the same attribute run; and the
// as-is's `<textblock\1>` backreference names the `(<p>)` group and so
// wrote the paragraph tag into the tag it was rebuilding; the attribute
// group is used here instead.

const maxMarkupPasses = 64

var (
	// Escaped forms, going out: <code> to &lt;[code]&gt; and back.
	reEscapedCodeTag      = regexp.MustCompile(`(?is)&lt;code((\s+[^\s]*?)|(\s*/))?&gt;`)
	reEscapedMoreTag      = regexp.MustCompile(`(?is)&lt;more((\s+[^\s]*?)|(\s*/))?&gt;`)
	reEscapedTextblockTag = regexp.MustCompile(`(?is)&lt;textblock((\s+[^\s]*?)|(\s*/))?&gt;`)

	reBracketCode      = regexp.MustCompile(`(?is)&lt;\[code((\s+[^\]]*)|(\s*/))?\]&gt;`)
	reBracketMore      = regexp.MustCompile(`(?is)&lt;\[more((\s+[^\]]*)|(\s*/))?\]&gt;`)
	reBracketTextblock = regexp.MustCompile(`(?is)&lt;\[textblock((\s+[^\]]*)|(\s*/))?\]&gt;`)

	// The greedy pairs: the as-is matched `(.*)(<code>)(.*)(</code>)(.*)`
	// and rewrote the last block first, then looped.
	reCodeBlock        = regexp.MustCompile(`(?is)^(.*)<code>(.*)</code>(.*)$`)
	reEscapedCodeBlock = regexp.MustCompile(`(?is)^(.*)&lt;code&gt;(.*)&lt;/code&gt;(.*)$`)

	reMoreTagOut      = regexp.MustCompile(`(?is)<more((\s+[^>]*)|(\s*/))>`)
	reTextblockTagOut = regexp.MustCompile(`(?is)<textblock((\s+[^>]*)|(\s*/))?>`)

	reEscapedMoreIn      = regexp.MustCompile(`(?is)(<p>)?&lt;more\s*/&gt;(</p>)?`)
	reEscapedTextblockIn = regexp.MustCompile(`(?is)(<p>)?&lt;textblock((\s+[^\]]*)|(\s*/))?&gt;(</p>)?`)
	reTextblockTagIn     = regexp.MustCompile(`(?is)<textblock((\s+[^>]*)|(\s*/))?>`)

	reNewline     = regexp.MustCompile(`\r?\n`)
	reMultiSpace  = regexp.MustCompile(`\s{2,}`)
	reParaTag     = regexp.MustCompile(`(?is)<p([^>]+)?>`)
	reBreakTag    = regexp.MustCompile(`(?is)<br([^>]+)?>`)
	reAnyTag      = regexp.MustCompile(`(?is)<[^>]+>`)
	reEntityLT    = regexp.MustCompile(`(?i)&lt;|&#60;`)
	reEntityGT    = regexp.MustCompile(`(?i)&gt;|&#62;`)
	reEntityQuote = regexp.MustCompile(`(?i)&quot;|&#34;`)
	reEntityApos  = regexp.MustCompile(`(?i)&apos;`)
	reEntityNbsp  = regexp.MustCompile(`(?i)&nbsp;|&#xA0;|&#160;`)
	reEntityAmp   = regexp.MustCompile(`&amp;|&#38;`)
)

// escapeMarkup prepares stored content for a rich-text client: markup
// tags the blog owns become bracketed forms, and each `<code>` block's
// body is HTML-escaped and its tags entity-escaped.
func escapeMarkup(s string) string {
	s = reEscapedCodeTag.ReplaceAllString(s, "&lt;[code$1]&gt;")
	s = reEscapedMoreTag.ReplaceAllString(s, "&lt;[more$1]&gt;")
	s = reEscapedTextblockTag.ReplaceAllString(s, "&lt;[textblock$1]&gt;")

	for i := 0; i < maxMarkupPasses; i++ {
		m := reCodeBlock.FindStringSubmatchIndex(s)
		if m == nil {
			break
		}
		inner := s[m[4]:m[5]]
		converted := ""
		if strings.TrimSpace(inner) != "" {
			converted = convertTextToHTML(inner)
		}
		s = s[m[2]:m[3]] + converted + s[m[6]:m[7]]
	}

	s = reMoreTagOut.ReplaceAllString(s, "<p>&lt;more$1&gt;</p>")
	s = reTextblockTagOut.ReplaceAllString(s, "<p>&lt;textblock$1&gt;</p>")
	return s
}

// unescapeMarkup is the other direction: what a rich-text client posted
// back becomes the stored form again.
func unescapeMarkup(s string) string {
	for i := 0; i < maxMarkupPasses; i++ {
		m := reEscapedCodeBlock.FindStringSubmatchIndex(s)
		if m == nil {
			break
		}
		inner := s[m[4]:m[5]]
		converted := ""
		if strings.TrimSpace(inner) != "" {
			converted = convertHTMLToText(inner)
		}
		s = s[m[2]:m[3]] + converted + s[m[6]:m[7]]
	}

	s = reEscapedMoreIn.ReplaceAllString(s, moreTag)
	s = reEscapedTextblockIn.ReplaceAllString(s, "<textblock$2>")

	s = reBracketCode.ReplaceAllString(s, "&lt;code$1&gt;")
	s = reBracketMore.ReplaceAllString(s, "&lt;more$1&gt;")
	s = reBracketTextblock.ReplaceAllString(s, "&lt;textblock$1&gt;")

	// Inside a textblock tag the quotes came back as entities.
	s = reTextblockTagIn.ReplaceAllStringFunc(s, func(tag string) string {
		return strings.ReplaceAll(strings.ReplaceAll(tag, "&quot;", `"`), "&QUOT;", `"`)
	})
	return s
}

// convertTextToHTML is the as-is's convertTextToHtml: a stored code block
// becomes an escaped one the editor will show verbatim.
func convertTextToHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = reEntityApos.ReplaceAllString(s, "''")
	s = reNewline.ReplaceAllStringFunc(s, func(nl string) string { return "<br/>" + nl })
	s = strings.ReplaceAll(s, "\t", "&#160;&#160;&#160;&#160;")
	return "&lt;code&gt;" + s + "&lt;/code&gt;"
}

// convertHTMLToText is the as-is's convertHtmlToText: what a rich-text
// editor made of a code block becomes plain text in a `<code>` block
// again. The steps and their order are the CFML's, including the
// collapse of runs of whitespace that leaves its own four-spaces-to-a-tab
// rule with nothing to do.
func convertHTMLToText(s string) string {
	s = reNewline.ReplaceAllString(s, "")
	s = reMultiSpace.ReplaceAllString(s, " ")
	s = reParaTag.ReplaceAllString(s, "\r\n")
	s = reBreakTag.ReplaceAllString(s, "\r\n")
	s = reAnyTag.ReplaceAllString(s, "")
	s = reEntityLT.ReplaceAllString(s, "<")
	s = reEntityGT.ReplaceAllString(s, ">")
	s = reEntityQuote.ReplaceAllString(s, `"`)
	s = reEntityApos.ReplaceAllString(s, "''")
	s = reEntityNbsp.ReplaceAllString(s, " ")
	s = reEntityAmp.ReplaceAllString(s, "&")
	s = strings.ReplaceAll(s, "    ", "\t")
	return "<code>" + s + "</code>"
}
