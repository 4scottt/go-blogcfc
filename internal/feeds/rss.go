// This file is the blog's RSS: `GET /rss`, BlogCFC's rss.cfm over
// org/camden/blog/blog.cfc's generateRSS (PLAN §8, §9 F01-F07). RSS 2.0
// with enclosures and iTunes tags by default, RSS 1.0 (RDF) with
// `version=1`, both built by text/template with one explicit escape
// function -- no feed library, as PLAN §5 asks.
package feeds

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/4scottt/go-blogcfc/internal/cache"
	"github.com/4scottt/go-blogcfc/internal/config"
	"github.com/4scottt/go-blogcfc/internal/render"
	"github.com/4scottt/go-blogcfc/internal/store"
	"github.com/4scottt/go-blogcfc/internal/web"
)

// maxItems is generateRSS's own cap: "Right now, we force this in.
// Useful to limit throughput of RSS feed." rss.cfm never passes
// maxEntries, so every feed the as-is served held at most 15 items,
// whatever `maxentries` said (PLAN §9 F03).
const maxItems = 15

// excerptChars is generateRSS's `excerpt` argument: how many characters
// of a tag-stripped body `mode=short` shows (PLAN §9 F02).
const excerptChars = 250

// rssTime is RSS 2.0's RFC 822 date with a four-digit year, written in
// the blog's zone as the as-is wrote it in the server's.
const rssTime = "Mon, 02 Jan 2006 15:04:05 -0700"

// rdfTime is RSS 1.0's dc:date, the same W3C stamp the sitemap uses.
const rdfTime = sitemapTime

// RSS serves /rss.
type RSS struct {
	// Cache is the blog's one in-process cache (PLAN §11 "Caching").
	// main.go sets it; nil means nothing is cached. Only the unfiltered
	// feed is cached, which is the one view rss.cfm's scopecache held.
	Cache *cache.Cache

	cfg      *config.Config
	store    *store.Store
	settings *config.Settings
	tmpl     *template.Template

	// now is the clock, so the golden tests get a fixed channel pubDate.
	now func() time.Time
}

// NewRSS builds the module. It panics if the feed templates do not
// parse, which is a build-time mistake rather than a runtime one.
func NewRSS(cfg *config.Config, st *store.Store, settings *config.Settings) *RSS {
	t := template.New("feeds").Funcs(template.FuncMap{"x": escapeXML})
	template.Must(t.New("rss2").Parse(rss2Template))
	template.Must(t.New("rss1").Parse(rss1Template))
	return &RSS{cfg: cfg, store: st, settings: settings, tmpl: t, now: time.Now}
}

// Routes registers the one feed endpoint. Every shape of feed is a query
// on it, as rss.cfm was (PLAN §8).
func (m *RSS) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /rss", m.handleRSS)
}

// base is the blog's own URL without a trailing slash. Every link in a
// feed is built from it, never from the request's Host (PLAN §6).
func (m *RSS) base() string { return strings.TrimRight(m.cfg.BlogBaseURL, "/") }

// feedRequest is rss.cfm's reading of the query string: the mode, the
// version, and the one optional mode2 filter.
type feedRequest struct {
	full    bool // mode=full
	version int  // 1 or 2
	// mode2 is "", "day", "month", "cat" or "entry".
	mode2 string
	from  *time.Time
	to    *time.Time
	cats  []string
	entry string
	// cacheable is rss.cfm's rule: only the unfiltered feed is cached.
	cacheable bool
}

// parseRequest reads the query the way rss.cfm did: an unknown mode is
// short, an unknown version is 2, and a mode2 whose parameters do not
// parse simply does not filter.
func (m *RSS) parseRequest(q url.Values) feedRequest {
	req := feedRequest{version: 2}
	if strings.EqualFold(q.Get("mode"), "full") {
		req.full = true
	}
	if strings.TrimSpace(q.Get("version")) == "1" {
		req.version = 1
	}

	mode2 := strings.ToLower(strings.TrimSpace(q.Get("mode2")))
	// The as-is disabled its cache the moment mode2 was present at all,
	// even when the filter it named was nonsense.
	req.cacheable = q.Get("mode2") == ""

	switch mode2 {
	case "day", "month":
		year, okYear := numeric(q.Get("year"))
		month, okMonth := numeric(q.Get("month"))
		day, okDay := numeric(q.Get("day"))
		if !okYear || !okMonth || month < 1 || month > 12 {
			break
		}
		if mode2 == "day" && (!okDay || day < 1 || day > 31) {
			break
		}
		if mode2 == "month" {
			day = 0
		}
		from, to := monthRange(year, time.Month(month), day, m.settings.Timezone())
		req.mode2, req.from, req.to = mode2, &from, &to
	case "cat":
		// A list, as the as-is allowed: `catid=a,b`. The as-is capped each
		// id at 35 characters for CFML's UUIDs; ours are 36-character
		// UUIDs, so there is no cap here.
		for _, raw := range strings.Split(q.Get("catid"), ",") {
			if id := strings.TrimSpace(raw); id != "" {
				req.cats = append(req.cats, id)
			}
		}
		if len(req.cats) > 0 {
			req.mode2 = "cat"
		}
	case "entry":
		if id := strings.TrimSpace(q.Get("entry")); id != "" {
			req.mode2, req.entry = "entry", id
		}
	}
	return req
}

// numeric is CFML's val() for a date segment, minus its habit of
// accepting "12abc": a segment either is a number or is ignored.
func numeric(s string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// monthRange is the archive window a day or month filter selects, in the
// blog's zone: the as-is compared year(), month() and day() of `posted`
// shifted by the blog's offset, which is this window in UTC.
func monthRange(year int, month time.Month, day int, loc *time.Location) (time.Time, time.Time) {
	from := time.Date(year, month, 1, 0, 0, 0, 0, loc)
	to := from.AddDate(0, 1, 0)
	if day > 0 {
		from = time.Date(year, month, day, 0, 0, 0, 0, loc)
		to = from.AddDate(0, 0, 1)
	}
	return from.UTC(), to.Add(-time.Second).UTC()
}

// feedDoc is a rendered feed and the stamp its headers carry.
type feedDoc struct {
	body    string
	lastMod time.Time
}

// handleRSS answers the feed: build it (through the cache when it is the
// unfiltered view), then the conditional-GET dance rss.cfm's aggregator
// support added (PLAN §9 F06).
func (m *RSS) handleRSS(w http.ResponseWriter, r *http.Request) {
	req := m.parseRequest(r.URL.Query())

	doc, err := m.document(r.Context(), req)
	if err != nil {
		slog.Error("rss: build", "error", err)
		http.Error(w, "feed unavailable", http.StatusInternalServerError)
		return
	}

	etag := `"` + hashBody(doc.body) + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Last-Modified", doc.lastMod.UTC().Format(http.TimeFormat))
	if req.version == 1 {
		w.Header().Set("Content-Type", "application/rdf+xml; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	}

	if notModified(r, etag, doc.lastMod) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	_, _ = w.Write([]byte(doc.body))
}

// notModified is RFC 9110's precedence: a validator beats a date, and
// an aggregator that sends only a date still gets its 304. The as-is
// wanted both headers to match and so answered 200 to every aggregator
// that sent one of them.
func notModified(r *http.Request, etag string, lastMod time.Time) bool {
	if inm := r.Header.Get("If-None-Match"); inm != "" {
		for _, tag := range strings.Split(inm, ",") {
			tag = strings.TrimSpace(tag)
			if tag == "*" || strings.TrimPrefix(tag, "W/") == etag {
				return true
			}
		}
		return false
	}
	if ims := r.Header.Get("If-Modified-Since"); ims != "" {
		if t, err := http.ParseTime(ims); err == nil {
			return !lastMod.Truncate(time.Second).After(t)
		}
	}
	return false
}

// hashBody is the ETag's source: the document itself, so any change to
// the feed changes the tag. rss.cfm hashed the newest item's pubDate,
// which never moved when an entry was edited (PLAN §9 F06).
func hashBody(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// document builds the feed, through the cache when this is the view the
// as-is cached: the plain feed, keyed by mode and version as rss.cfm's
// cachename was.
func (m *RSS) document(ctx context.Context, req feedRequest) (feedDoc, error) {
	if !req.cacheable || m.Cache == nil {
		return m.build(ctx, req)
	}
	key := "feeds:rss:" + strconv.Itoa(req.version)
	if req.full {
		key += ":full"
	} else {
		key += ":short"
	}
	return cache.Value(m.Cache, key, func() (feedDoc, error) { return m.build(ctx, req) })
}

// build reads the entries and renders the document.
func (m *RSS) build(ctx context.Context, req feedRequest) (feedDoc, error) {
	entries, additionalTitle, err := m.entries(ctx, req)
	if err != nil {
		return feedDoc{}, err
	}

	loc := m.settings.Timezone()
	now := m.now().In(loc)
	lastMod := now
	if len(entries) > 0 {
		lastMod = entries[0].Posted
	}

	data := m.channel(ctx, req, entries, additionalTitle, now, loc)
	name := "rss2"
	if req.version == 1 {
		name = "rss1"
	}
	var b strings.Builder
	if err := m.tmpl.ExecuteTemplate(&b, name, data); err != nil {
		return feedDoc{}, err
	}
	return feedDoc{body: b.String(), lastMod: lastMod}, nil
}

// entries is getEntries with rss.cfm's params: live only (PLAN §11
// "Three entry states", §9 F07), newest first, at most 15, filtered by
// the mode2 the query asked for.
func (m *RSS) entries(ctx context.Context, req feedRequest) ([]store.Entry, string, error) {
	if req.mode2 == "entry" {
		e, err := m.store.GetEntry(ctx, req.entry)
		if errors.Is(err, store.ErrNotFound) {
			return nil, "", nil
		}
		if err != nil {
			return nil, "", err
		}
		if !e.Live(m.now()) {
			return nil, "", nil
		}
		return []store.Entry{*e}, "", nil
	}

	f := store.EntryFilter{
		LiveOnly: true,
		Sort:     "posted",
		Desc:     true,
		Limit:    maxItems,
		From:     req.from,
		To:       req.to,
	}

	var additionalTitle string
	if req.mode2 == "cat" {
		f.CategoryIDs = req.cats
		// The as-is appended every named category's name to the channel
		// title, and shrugged off an id that matched nothing.
		for _, id := range req.cats {
			c, err := m.store.GetCategory(ctx, id)
			if err != nil {
				continue
			}
			additionalTitle += " - " + c.Name
		}
	}

	entries, _, err := m.store.ListEntries(ctx, f)
	if err != nil {
		return nil, "", err
	}
	return entries, additionalTitle, nil
}

// feedData is one rendered channel.
type feedData struct {
	Title          string
	Link           string
	Description    string
	Language       string
	PubDate        string
	LastBuildDate  string
	Generator      string
	Docs           string
	ManagingEditor string
	WebMaster      string
	ITunes         itunesChannel
	Items          []feedItem
}

// itunesChannel is the podcast block generateRSS wrote from the blog's
// iTunes settings (PLAN §10).
type itunesChannel struct {
	Subtitle string
	Summary  string
	Keywords string
	Author   string
	Image    string
	Explicit string
	Email    string
}

// feedItem is one entry as both feed versions need it.
type feedItem struct {
	Title       string
	Link        string
	Description string
	PubDate     string
	DCDate      string
	Author      string
	Categories  []string
	Subject     string
	Enclosure   *feedEnclosure
}

// feedEnclosure is the entry's attached file, plus the iTunes tags that
// only an audio/mpeg enclosure earns (PLAN §9 F04).
type feedEnclosure struct {
	URL    string
	Length string
	Type   string
	ITunes *itunesItem
}

// itunesItem is the per-entry podcast block.
type itunesItem struct {
	Author   string
	Explicit string
	Duration string
	Keywords string
	Subtitle string
	Summary  string
	Image    string
}

// channel assembles the template's data from the settings and the
// entries.
func (m *RSS) channel(ctx context.Context, req feedRequest, entries []store.Entry,
	additionalTitle string, now time.Time, loc *time.Location) feedData {

	base := m.base()
	explicit := "no"
	if m.settings.ITunesExplicit() {
		explicit = "yes"
	}
	data := feedData{
		Title:       m.settings.BlogTitle() + additionalTitle,
		Link:        base,
		Description: m.settings.BlogDescription(),
		Language:    languageTag(m.settings.Locale()),
		PubDate:     now.Format(rssTime),
		// generateRSS stamped lastBuildDate with the newest entry, and
		// blew up on an empty blog doing it; here an empty feed is stamped
		// now, as the sitemap's root is.
		LastBuildDate:  now.Format(rssTime),
		Generator:      "go-blogcfc",
		Docs:           "http://blogs.law.harvard.edu/tech/rss",
		ManagingEditor: m.settings.OwnerEmail(),
		WebMaster:      m.settings.OwnerEmail(),
		ITunes: itunesChannel{
			Subtitle: m.settings.ITunesSubtitle(),
			Summary:  m.settings.ITunesSummary(),
			Keywords: m.settings.ITunesKeywords(),
			Author:   m.settings.ITunesAuthor(),
			Image:    m.settings.ITunesImage(),
			Explicit: explicit,
			Email:    m.settings.OwnerEmail(),
		},
	}
	if len(entries) > 0 {
		data.LastBuildDate = entries[0].Posted.In(loc).Format(rssTime)
	}

	// The textblocks a body may expand, read once for the whole feed.
	var textblocks func(string) (string, bool)
	if blocks, err := m.store.TextblockMap(ctx); err == nil {
		textblocks = func(label string) (string, bool) {
			v, ok := blocks[label]
			return v, ok
		}
	}

	author := m.settings.OwnerEmail()
	if name := m.settings.ITunesAuthor(); name != "" {
		author += " (" + name + ")"
	}

	for _, e := range entries {
		posted := e.Posted.In(loc)
		item := feedItem{
			Title:       e.Title,
			Link:        web.EntryURL(base, e, loc),
			Description: m.describe(e, req.full, textblocks, base),
			PubDate:     posted.Format(rssTime),
			DCDate:      posted.Format(rdfTime),
			Author:      author,
		}
		for _, c := range e.Categories {
			item.Categories = append(item.Categories, c.Name)
		}
		// generateRSS built its RSS 1.0 dc:subject in a variable it never
		// reset, so every item carried every earlier item's categories
		// too. Here the list is the item's own.
		item.Subject = strings.Join(item.Categories, ",")

		if u := enclosureURL(base, e); u != "" {
			enc := &feedEnclosure{URL: u, Length: strconv.FormatInt(e.FileSize, 10), Type: e.MimeType}
			if strings.EqualFold(strings.TrimSpace(e.MimeType), "audio/mpeg") {
				enc.ITunes = &itunesItem{
					Author:   m.settings.ITunesAuthor(),
					Explicit: explicit,
					Duration: e.Duration,
					Keywords: e.Keywords,
					Subtitle: e.Subtitle,
					Summary:  e.Summary,
					Image:    m.settings.ITunesImage(),
				}
			}
			item.Enclosure = enc
		}
		data.Items = append(data.Items, item)
	}
	return data
}

// describe is the item's description: generateRSS's rule, which is not
// quite "short means an excerpt". A short feed excerpts only a body that
// is long enough to need it; anything shorter is sent whole, morebody
// and all, exactly as a full feed sends it (PLAN §9 F02).
func (m *RSS) describe(e store.Entry, full bool, textblocks func(string) (string, bool), base string) string {
	if !full {
		stripped := []rune(stripTags(e.Body))
		if len(stripped) >= excerptChars {
			return string(stripped[:excerptChars]) + "..."
		}
	}
	opts := render.Options{
		Enclosure:    e.Enclosure,
		EnclosureURL: enclosureURL(base, e),
		MimeType:     e.MimeType,
		Textblocks:   textblocks,
	}
	body := string(render.Entry(e.Body, opts))
	if e.MoreBody != "" {
		body += string(render.Entry(e.MoreBody, render.Options{Textblocks: textblocks}))
	}
	return body
}

// tagPattern is generateRSS's own `<[^>]*>`: crude, and the excerpt it
// produces is the as-is's excerpt.
var tagPattern = regexp.MustCompile(`<[^>]*>`)

// stripTags removes the markup from a body for the short excerpt.
func stripTags(body string) string { return tagPattern.ReplaceAllString(body, "") }

// enclosureURL is where an entry's attached file lives: the same
// `{base}/enclosures/{file}` the site's own pages point at (PLAN §8).
// An entry with no enclosure has no URL, and so no enclosure element.
func enclosureURL(base string, e store.Entry) string {
	name := path.Base(filepath.ToSlash(strings.TrimSpace(e.Enclosure)))
	if strings.TrimSpace(e.Enclosure) == "" || name == "." || name == "/" {
		return ""
	}
	return base + "/enclosures/" + url.PathEscape(name)
}

// languageTag is the channel's <language>: generateRSS's
// `replace(lcase(locale),'_','-','one')`, so `en_US` becomes `en-us`.
func languageTag(locale string) string {
	return strings.Replace(strings.ToLower(strings.TrimSpace(locale)), "_", "-", 1)
}

// escapeXML is the one escape in these templates, xmlFormat's job: text
// nodes and attribute values alike. Non-ASCII passes through as UTF-8,
// which the prolog declares; a newline inside a full body stays a
// newline, which xml.EscapeText would have turned into `&#xA;`.
var xmlEscaper = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	`"`, "&quot;",
	"'", "&apos;",
)

// escapeXML escapes one value for the feed, and drops the control
// characters XML 1.0 has no representation for.
func escapeXML(s string) string {
	return xmlEscaper.Replace(stripControl(s))
}

// stripControl removes the characters that are not legal in an XML 1.0
// document at all: a stray NUL in a body must not make the feed
// unparseable.
func stripControl(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			return r
		case r < 0x20, r == 0x7f:
			return -1
		}
		return r
	}, s)
}

// rss2Template is generateRSS's version 2 document. Three of its
// elements are deliberately not here (PLAN §7): `media:copyright` and
// the fixed `media:category` / `itunes:category` trees, which named one
// 2008 podcast's owner and taxonomy and are backed by no setting of
// ours; and the unused `rdf` and `cc` namespace declarations.
const rss2Template = `<?xml version="1.0" encoding="utf-8"?>
<rss version="2.0" xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd" xmlns:media="http://search.yahoo.com/mrss/">
<channel>
<title>{{x .Title}}</title>
<link>{{x .Link}}</link>
<description>{{x .Description}}</description>
<language>{{x .Language}}</language>
<pubDate>{{x .PubDate}}</pubDate>
<lastBuildDate>{{x .LastBuildDate}}</lastBuildDate>
<generator>{{x .Generator}}</generator>
<docs>{{x .Docs}}</docs>
<managingEditor>{{x .ManagingEditor}}</managingEditor>
<webMaster>{{x .WebMaster}}</webMaster>
{{- with .ITunes}}
{{- if .Image}}
<media:thumbnail url="{{x .Image}}" />
{{- end}}
<media:keywords>{{x .Keywords}}</media:keywords>
<itunes:subtitle>{{x .Subtitle}}</itunes:subtitle>
<itunes:summary>{{x .Summary}}</itunes:summary>
<itunes:keywords>{{x .Keywords}}</itunes:keywords>
<itunes:author>{{x .Author}}</itunes:author>
<itunes:owner>
<itunes:email>{{x .Email}}</itunes:email>
<itunes:name>{{x .Author}}</itunes:name>
</itunes:owner>
{{- if .Image}}
<itunes:image href="{{x .Image}}" />
{{- end}}
<itunes:explicit>{{x .Explicit}}</itunes:explicit>
{{- end}}
{{- if .ITunes.Image}}
<image>
<url>{{x .ITunes.Image}}</url>
<title>{{x .Title}}</title>
<link>{{x .Link}}</link>
</image>
{{- end}}
{{- range .Items}}
<item>
<title>{{x .Title}}</title>
<link>{{x .Link}}</link>
<description>{{x .Description}}</description>
{{- range .Categories}}
<category>{{x .}}</category>
{{- end}}
<pubDate>{{x .PubDate}}</pubDate>
<guid isPermaLink="true">{{x .Link}}</guid>
<author>{{x .Author}}</author>
{{- with .Enclosure}}
<enclosure url="{{x .URL}}" length="{{x .Length}}" type="{{x .Type}}" />
{{- with .ITunes}}
<itunes:author>{{x .Author}}</itunes:author>
<itunes:explicit>{{x .Explicit}}</itunes:explicit>
<itunes:duration>{{x .Duration}}</itunes:duration>
<itunes:keywords>{{x .Keywords}}</itunes:keywords>
<itunes:subtitle>{{x .Subtitle}}</itunes:subtitle>
<itunes:summary>{{x .Summary}}</itunes:summary>
{{- if .Image}}
<itunes:image href="{{x .Image}}" />
{{- end}}
{{- end}}
{{- end}}
</item>
{{- end}}
</channel>
</rss>
`

// rss1Template is generateRSS's version 1 document: the RDF shape, with
// dc:date and dc:subject. The as-is wrote its items inside <channel>'s
// sibling position by concatenation, which is where they belong, and
// that is where they are here.
const rss1Template = `<?xml version="1.0" encoding="utf-8"?>
<rdf:RDF
	xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"
	xmlns:dc="http://purl.org/dc/elements/1.1/"
	xmlns="http://purl.org/rss/1.0/"
>
<channel rdf:about="{{x .Link}}">
<title>{{x .Title}}</title>
<description>{{x .Description}}</description>
<link>{{x .Link}}</link>
<items>
<rdf:Seq>
{{- range .Items}}
<rdf:li rdf:resource="{{x .Link}}" />
{{- end}}
</rdf:Seq>
</items>
</channel>
{{- range .Items}}
<item rdf:about="{{x .Link}}">
<title>{{x .Title}}</title>
<description>{{x .Description}}</description>
<link>{{x .Link}}</link>
<dc:date>{{x .DCDate}}</dc:date>
<dc:subject>{{x .Subject}}</dc:subject>
</item>
{{- end}}
</rdf:RDF>
`
