package admin

import (
	"context"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/4scottt/go-blogcfc/internal/auth"
	renderpkg "github.com/4scottt/go-blogcfc/internal/render"
	"github.com/4scottt/go-blogcfc/internal/store"
)

// The editor's Preview (PLAN §9 A10, admin/entry.cfm's `form.preview`
// branch): the entry as a reader would meet it, rendered inside the
// admin frame and saved nowhere. The as-is re-posted every form field
// as a hidden input so Return and Save carried the typed values back;
// this does the same, which is also how an enclosure uploaded just
// before the preview survives it.

// hiddenField is one value carried through the preview.
type hiddenField struct {
	Name  string
	Value string
}

// previewPage is preview.html's own data.
type previewPage struct {
	Action     string
	EntryTitle string
	Posted     string
	Author     string
	Categories []string
	Body       template.HTML
	Hidden     []hiddenField
	Enclosure  string
}

// entryPreview renders the editor's preview. Nothing is written: the
// caller has already stored an uploaded enclosure, and everything else
// goes back into the form.
func (m *Module) entryPreview(w http.ResponseWriter, r *http.Request, p entryFormPage) {
	ctx := r.Context()
	u := auth.UserFrom(ctx)

	opts := renderpkg.Options{
		Enclosure:  p.Enclosure,
		MimeType:   p.Mimetype,
		Textblocks: m.textblockLookup(ctx),
	}
	if p.Enclosure != "" {
		opts.EnclosureURL = "/enclosures/" + url.PathEscape(p.Enclosure)
	}

	body, more := splitMore(p.Body)
	rendered := renderpkg.Entry(body, opts)
	if more != "" {
		// morebody is rendered without the enclosure decoration, as the
		// public entry view does (internal/web, PLAN §9 R02).
		rendered += renderpkg.Entry(more, renderpkg.Options{Textblocks: opts.Textblocks})
	}

	pv := previewPage{
		Action:     p.Action,
		EntryTitle: p.Title,
		Posted:     p.Posted,
		Author:     authorName(u),
		Body:       rendered,
		Enclosure:  p.Enclosure,
		Hidden:     previewHidden(p),
	}
	for _, c := range p.Categories {
		if p.Selected[c.ID] {
			pv.Categories = append(pv.Categories, c.Name)
		}
	}

	data := m.newPageData(r, "Entry Preview")
	data.Page = pv
	render(w, "preview.html", data)
}

// previewHidden is every value the editor submitted, ready to be posted
// again by the preview's own form.
func previewHidden(p entryFormPage) []hiddenField {
	out := []hiddenField{
		{"title", p.Title},
		{"body", p.Body},
		{"alias", p.Alias},
		{"posted", p.Posted},
		{"newcategory", p.NewCategory},
		{"subtitle", p.Subtitle},
		{"keywords", p.Keywords},
		{"summary", p.Summary},
		{"duration", p.Duration},
		{"oldenclosure", p.Enclosure},
		{"oldfilesize", strconv.FormatInt(p.Filesize, 10)},
		{"oldmimetype", p.Mimetype},
	}
	if p.AllowComments {
		out = append(out, hiddenField{"allowcomments", "1"})
	}
	if p.SendEmail {
		out = append(out, hiddenField{"sendemail", "1"})
	}
	if p.Released {
		out = append(out, hiddenField{"released", "1"})
	}
	for _, c := range p.Categories {
		if p.Selected[c.ID] {
			out = append(out, hiddenField{"categories", c.ID})
		}
	}
	for _, rel := range p.Related {
		out = append(out, hiddenField{"related", rel.ID})
	}
	return out
}

// splitMore cuts a typed body at the first <more/>, which is what the
// public list and the permalink show apart (PLAN §11).
func splitMore(typed string) (body, more string) {
	idx := indexMore(typed)
	if idx < 0 {
		return typed, ""
	}
	return strings.TrimSpace(typed[:idx]), strings.TrimSpace(typed[idx+len(moreTag):])
}

// authorName is how the preview signs the entry: the user's name when
// they have one, their username otherwise (blog.cfc's getNameForUser).
func authorName(u *store.User) string {
	if u == nil {
		return ""
	}
	if strings.TrimSpace(u.Name) != "" {
		return u.Name
	}
	return u.Username
}

// textblockLookup resolves `<textblock label="x">` for the preview, so
// what the author sees is what the site will render (PLAN §9 A22). A
// table that cannot be read resolves nothing rather than failing.
func (m *Module) textblockLookup(ctx context.Context) func(string) (string, bool) {
	blocks, err := m.store.TextblockMap(ctx)
	if err != nil {
		slog.Error("admin: textblock lookup failed", "error", err)
		return nil
	}
	if len(blocks) == 0 {
		return nil
	}
	return func(label string) (string, bool) {
		body, ok := blocks[label]
		return body, ok
	}
}
