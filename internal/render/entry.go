// Package render turns entry content into HTML: BlogCFC's renderEntry
// pipeline (org/camden/blog/blog.cfc), in the order the CFML ran it -
// code blocks, render plugins, enclosure decorations, textblocks,
// paragraph wrapping (PLAN §9 R01-R03) - plus the slugifier (R04) and
// the Gravatar URL (§11).
//
// Two of BlogCFC's render plugins are deliberately not here (PLAN §7,
// "Dropped"): `<include ...>`, which read an arbitrary file off the
// server, and `<amazonbox ...>`, whose iframe widgets Amazon has
// discontinued. Nothing takes their place: a body still carrying one of
// those tags keeps it, verbatim and inert.
//
// An entry body is trusted HTML written in the admin, so it is never
// escaped. Everything this package builds from data - the enclosure URL,
// the print view's code - is escaped here.
package render

import (
	"html"
	"html/template"
	"regexp"
	"strconv"
	"strings"
)

// Options are renderEntry's other arguments (PLAN §9 R01-R03).
type Options struct {
	// Enclosure is the stored enclosure's file name. BlogCFC decided an
	// enclosure's kind by this extension; MimeType wins when it is set.
	Enclosure string
	// EnclosureURL is the URL the decoration points at. Empty means no
	// decoration: there is nothing to show.
	EnclosureURL string
	// MimeType is the enclosure's type, e.g. "image/png", "audio/mpeg".
	MimeType string
	// Textblocks resolves a `<textblock label="x">` tag. A false second
	// result leaves the tag in the body, where it is visible rather than
	// silently swallowed. Nil resolves nothing.
	Textblocks func(label string) (string, bool)
	// IgnoreParagraphs skips the paragraph formatter, as BlogCFC's
	// ignoreParagraphFormat argument did.
	IgnoreParagraphs bool
	// ForPrint renders code blocks for the print view.
	ForPrint bool
}

// Entry renders an entry, page or morebody. The result is trusted HTML.
func Entry(body string, opts Options) template.HTML {
	// The placeholders below are NULs; a body cannot be allowed to forge
	// one. They are not legal in HTML text either way.
	body = strings.ReplaceAll(body, "\x00", "")

	// 1. Code blocks, parked behind placeholders until the end.
	body, blocks := expandCodeBlocks(body, opts.ForPrint)
	// 2. BlogCFC's render plugins ran here. Both are dropped; see the
	//    package comment.
	// 3. Enclosure decorations.
	body = decorateEnclosure(body, opts)
	// 4. Textblocks.
	body = expandTextblocks(body, opts.Textblocks)
	// 5. Paragraphs.
	if !opts.IgnoreParagraphs {
		body = paragraphFormat(body)
	}
	return template.HTML(restoreCodeBlocks(body, blocks))
}

// Print renders for the print view (`/print/{id}`): code blocks escaped
// into `pre.codePrint` rather than highlighted, as BlogCFC's
// renderEntry(..., printformat=true) did.
func Print(body string, opts Options) template.HTML {
	opts.ForPrint = true
	return Entry(body, opts)
}

// decorateEnclosure prepends an image or appends an audio player (PLAN
// §9 R02). BlogCFC prepended a `<div>` for the Flash player to attach
// to; the player is gone, and an `<audio>` element reads better after
// the text than before it (PLAN §7, "Flash MP3 player").
func decorateEnclosure(body string, opts Options) string {
	if opts.EnclosureURL == "" {
		return body
	}
	src := html.EscapeString(opts.EnclosureURL)
	switch enclosureKind(opts) {
	case "image":
		return `<div class="autoImage"><img src="` + src + `" alt=""></div>` + body
	case "audio":
		return body + `<audio controls src="` + src + `"></audio>`
	}
	return body
}

// enclosureKind is the mimetype's family, or, when no mimetype was
// stored, BlogCFC's own test: the file's extension against gif,jpg,png
// and mp3.
func enclosureKind(opts Options) string {
	switch mime := strings.ToLower(strings.TrimSpace(opts.MimeType)); {
	case strings.HasPrefix(mime, "image/"):
		return "image"
	case mime == "audio/mpeg", mime == "audio/mp3":
		return "audio"
	case mime != "":
		return ""
	}
	name := strings.ToLower(opts.Enclosure)
	if i := strings.LastIndex(name, "."); i >= 0 {
		switch name[i+1:] {
		case "gif", "jpg", "jpeg", "png":
			return "image"
		case "mp3":
			return "audio"
		}
	}
	return ""
}

// textblockPattern is BlogCFC's tbRegex, with `[[:space:]]` spelled the
// Go way: `<textblock label="some label">`, the label non-greedy.
var textblockPattern = regexp.MustCompile(`(?i)<textblock[\s]+label[\s]*=[\s]*"(.*?)">`)

// expandTextblocks replaces each `<textblock label="x">` with the
// block's content. BlogCFC replaced an unknown label with the empty
// string its query returned; we leave the tag alone instead, so a typo
// shows up in the page rather than deleting itself.
func expandTextblocks(body string, lookup func(string) (string, bool)) string {
	if lookup == nil {
		return body
	}
	return textblockPattern.ReplaceAllStringFunc(body, func(tag string) string {
		m := textblockPattern.FindStringSubmatch(tag)
		if m == nil {
			return tag
		}
		if content, ok := lookup(m[1]); ok {
			return content
		}
		return tag
	})
}

// paragraphFormat is BlogCFC's XHTMLParagraphFormat (PLAN §9 R03):
//
//	REReplace("<p>" & string & "</p>", "\r+\n\r+\n", "</p><p>", "ALL")
//
// The whole body is wrapped in one `<p>` and every blank line closes it
// and opens the next. Nothing else happens: a single newline stays a
// newline (no `<br>`), and a body that already contains its own `<p>`
// tags is wrapped anyway, exactly as the CFML does - the auto-detection
// of an existing `<p>` is commented out in blog.cfc, and the only way to
// skip the pass is the ignoreParagraphFormat argument, which is this
// package's IgnoreParagraphs. BlogCFC has no `<!-- ignoreparagraphs -->`
// marker.
//
// One deviation: the CFML pattern needs carriage returns, so a body
// stored with Unix newlines got no paragraphs at all. Line endings are
// normalised first here, so a blank line is a blank line.
func paragraphFormat(body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")
	// ReplaceAll consumes each pair of newlines once, left to right,
	// which is what the CFML's "ALL" replace does: four newlines in a
	// row leave an empty paragraph behind, as they do in BlogCFC.
	return "<p>" + strings.ReplaceAll(body, "\n\n", "</p><p>") + "</p>"
}

// placeholder is the stand-in for the nth rendered code block.
func placeholder(n int) string { return "\x00code:" + strconv.Itoa(n) + "\x00" }

// restoreCodeBlocks puts the rendered code blocks back.
func restoreCodeBlocks(body string, blocks []string) string {
	for i, block := range blocks {
		body = strings.Replace(body, placeholder(i), block, 1)
	}
	return body
}
