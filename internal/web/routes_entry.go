package web

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/4scottt/go-blogcfc/internal/i18n"
	"github.com/4scottt/go-blogcfc/internal/render"
	"github.com/4scottt/go-blogcfc/internal/store"
)

// routesEntry registers what completes the entry page: the static pages
// (/page/{alias}) and the print view (/print/{id}). The view counter, the
// related entries and the comment list hang off the entry view itself and
// need no route of their own.
func (m *Module) routesEntry(mux *http.ServeMux) {
	mux.HandleFunc("GET /page/{$}", m.handlePageRoot)
	mux.HandleFunc("GET /page/{alias}", m.handlePage)
	mux.HandleFunc("GET /print/{id}", m.handlePrint)
}

// seenCookie carries the set of entries this visitor has already been
// counted for (PLAN §11, "Views"). BlogCFC kept the same set in
// `session.viewedpages`; a signed cookie holds it without a session
// store, and the value is `id,id,...|hex(HMAC-SHA256)` so a visitor can
// read it but not stuff it with somebody else's ids.
const seenCookie = "gbc_seen"

// seenMax caps the set. The oldest ids fall off the front, so a visitor
// who reads more than this eventually counts a second view on the entry
// they read first -- which is what a session that had expired would have
// done anyway.
const seenMax = 200

// seenLifetime is how long the cookie lives. BlogCFC's set died with the
// session; a visitor coming back next month may count again.
const seenLifetime = 30 * 24 * time.Hour

// countView adds one view to an entry, once per visitor (PLAN §9 P12).
// It is called from the single-entry view only -- never from a listing,
// never from the print view -- and never twice in one request: a request
// that has already written the cookie is one that has already counted.
//
// The as-is counted twice on some paths: index.cfm called logView while
// tags/layout.cfm asked getEntry to log the same read (the `dontLog`
// dance). Here one call site owns it (PLAN §7, "Bugs fixed").
func (m *Module) countView(w http.ResponseWriter, r *http.Request, entryID string) {
	if entryID == "" || strings.ContainsAny(entryID, ",|") {
		return
	}
	for _, set := range w.Header().Values("Set-Cookie") {
		if strings.HasPrefix(set, seenCookie+"=") {
			return
		}
	}
	seen := m.readSeen(r)
	for _, id := range seen {
		if id == entryID {
			return
		}
	}
	if err := m.store.IncrementViews(r.Context(), entryID); err != nil {
		slog.Error("web: view count failed", "entry", entryID, "error", err)
		return
	}
	seen = append(seen, entryID)
	if len(seen) > seenMax {
		seen = seen[len(seen)-seenMax:]
	}
	m.writeSeen(w, seen)
}

// readSeen returns the ids the visitor's cookie carries, or nothing when
// there is no cookie, the signature does not check out, or the value is
// malformed. A visitor who forges one only stops their own views being
// counted, which is why an empty SessionSecret is not fatal here.
func (m *Module) readSeen(r *http.Request) []string {
	c, err := r.Cookie(seenCookie)
	if err != nil || c.Value == "" {
		return nil
	}
	raw, sig, ok := strings.Cut(c.Value, "|")
	if !ok || !hmac.Equal([]byte(sig), []byte(m.signSeen(raw))) {
		return nil
	}
	var out []string
	for _, id := range strings.Split(raw, ",") {
		if id != "" {
			out = append(out, id)
		}
	}
	return out
}

// writeSeen signs the set back into the cookie.
func (m *Module) writeSeen(w http.ResponseWriter, seen []string) {
	raw := strings.Join(seen, ",")
	http.SetCookie(w, &http.Cookie{
		Name:     seenCookie,
		Value:    raw + "|" + m.signSeen(raw),
		Path:     "/",
		HttpOnly: true,
		Secure:   strings.HasPrefix(strings.ToLower(m.cfg.BlogBaseURL), "https://"),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(seenLifetime / time.Second),
	})
}

// signSeen is the cookie's HMAC, keyed with the secret the admin session
// signs with (PLAN §9 O06).
func (m *Module) signSeen(payload string) string {
	mac := hmac.New(sha256.New, []byte(m.cfg.SessionSecret))
	_, _ = mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// relatedViews is the block under an entry: the entries related to this
// one in either direction, live only, newest first (PLAN §9 P13). An
// entry with nothing related gets no block at all.
func (m *Module) relatedViews(ctx context.Context, entryID string) []relatedView {
	related, err := m.store.RelatedEntries(ctx, entryID)
	if err != nil {
		slog.Error("web: related entries failed", "entry", entryID, "error", err)
		return nil
	}
	base := m.base()
	loc := m.loc()
	locale := m.settings.Locale()
	out := make([]relatedView, 0, len(related))
	for _, e := range related {
		out = append(out, relatedView{
			Title:  e.Title,
			URL:    EntryURL(base, e, loc),
			Posted: i18n.FormatDate(locale, e.Posted.In(loc), i18n.StyleLong),
		})
	}
	return out
}

// commentViews is the comment list under an entry (PLAN §9 P14): the
// moderated comments oldest first, each with its anchor, its Gravatar
// when the blog allows them, the "{name} said on {date}" line and the
// body escaped, linkified and line-broken.
func (m *Module) commentViews(ctx context.Context, entryID, permalink string) []commentView {
	comments, err := m.store.ListComments(ctx, entryID, false)
	if err != nil {
		slog.Error("web: comments failed", "entry", entryID, "error", err)
		return nil
	}
	loc := m.loc()
	locale := m.settings.Locale()
	said := m.bundle.T("said")
	gravatars := m.settings.AllowGravatars()
	defaultAvatar := m.base() + "/static/images/gravatar.gif"

	out := make([]commentView, 0, len(comments))
	for i, c := range comments {
		anchor := "c" + c.ID
		v := commentView{
			Anchor:  anchor,
			URL:     permalink + "#" + anchor,
			Number:  i + 1,
			Name:    c.Name,
			Website: externalURL(c.Website),
			Said:    said,
			Posted:  i18n.FormatDate(locale, c.Posted.In(loc), i18n.StylePosted),
			Body:    commentBody(c.Comment),
			Alt:     i%2 == 1,
		}
		if gravatars {
			v.AvatarURL = render.Gravatar(c.Email, 64, defaultAvatar)
		}
		out = append(out, v)
	}
	return out
}

// commentBody renders one comment's text. BlogCFC ran
// `ParagraphFormat2(replaceLinks(comment))` and escaped nothing on the
// way out (index.cfm), which let a stored comment put markup on the
// page; the escaping happens inside replaceLinks here (PLAN §7, "Bugs
// fixed"). Everything left is a tag this package wrote.
func commentBody(comment string) template.HTML {
	return template.HTML(paragraphFormat2(replaceLinks(comment))) //nolint:gosec // escaped above
}

// handlePageRoot is `/page/` with nothing after it: page.cfm sent that
// home rather than showing anything.
func (m *Module) handlePageRoot(w http.ResponseWriter, r *http.Request) {
	m.redirectHome(w, r)
}

// handlePage serves `GET /page/{alias}` (PLAN §9 P17): the page's body
// through render.Entry, inside the layout when the page says so and bare
// when it does not. An alias nobody has goes home, as page.cfm did.
func (m *Module) handlePage(w http.ResponseWriter, r *http.Request) {
	alias := strings.TrimSpace(r.PathValue("alias"))
	if alias == "" {
		m.redirectHome(w, r)
		return
	}
	p, err := m.store.GetPageByAlias(r.Context(), alias)
	if errors.Is(err, store.ErrNotFound) {
		m.redirectHome(w, r)
		return
	}
	if err != nil {
		m.serverError(w, r, err)
		return
	}
	body := render.Entry(p.Body, render.Options{Textblocks: m.textblocks(r.Context())})
	if !p.ShowLayout {
		m.writeHTML(w, http.StatusOK, body)
		return
	}
	data := m.newPage(r, p.Title)
	data.Body = body
	m.render(w, "page.html", http.StatusOK, data)
}

// redirectHome is page.cfm's `<cflocation url="index.cfm">`: a 302 to the
// blog's own base URL, never to the request's host (FP O05).
func (m *Module) redirectHome(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, m.base()+"/", http.StatusFound)
}

// handlePrint serves `GET /print/{id}` (PLAN §9 P18): the title, the
// byline, and body plus morebody through render.Print, which escapes code
// blocks into `pre.codePrint`. It never counts a view.
//
// Two deviations from print.cfm: an id nobody has is a 404 rather than a
// redirect home, and the answer is HTML rather than the PDF `cfdocument`
// generated (PLAN §7, "Dropped": no PDF engine).
func (m *Module) handlePrint(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		m.notFound(w, r)
		return
	}
	e, err := m.store.GetEntry(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		m.notFound(w, r)
		return
	}
	if err != nil {
		m.serverError(w, r, err)
		return
	}
	if !e.Live(time.Now()) && !m.adminView(r) {
		m.notFound(w, r)
		return
	}

	base := m.base()
	textblocks := m.textblocks(r.Context())
	data := printData{
		BlogTitle:       m.settings.BlogTitle(),
		Title:           e.Title,
		CSSURL:          base + "/static/css/site.css",
		CodeCSSURL:      base + "/static/css/code.css",
		PostedAtLabel:   m.bundle.T("postedat"),
		Posted:          i18n.FormatDate(m.settings.Locale(), e.Posted.In(m.loc()), i18n.StylePosted),
		PostedByLabel:   m.bundle.T("postedby"),
		Author:          e.Username,
		CategoriesLabel: m.bundle.T("relatedcategories"),
		Body: render.Print(e.Body, render.Options{
			Enclosure:    e.Enclosure,
			EnclosureURL: enclosureURL(base, *e),
			MimeType:     e.MimeType,
			Textblocks:   textblocks,
		}),
	}
	if e.MoreBody != "" {
		data.MoreBody = render.Print(e.MoreBody, render.Options{Textblocks: textblocks})
	}
	for _, c := range e.Categories {
		data.Categories = append(data.Categories, c.Name)
	}
	m.renderBare(w, "print.html", http.StatusOK, data)
}

// externalURL keeps a commenter's own address only when it is one a
// browser may follow. BlogCFC linked whatever the field held, so a
// `javascript:` website was a script on every reader's entry page
// (PLAN §7, "Bugs fixed").
func externalURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return u.String()
}
