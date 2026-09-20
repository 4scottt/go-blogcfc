package feeds

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/4scottt/go-blogcfc/internal/cache"
	"github.com/4scottt/go-blogcfc/internal/config"
	"github.com/4scottt/go-blogcfc/internal/store"
	"github.com/4scottt/go-blogcfc/internal/testdb"
)

// feedNow is the clock every feed test runs on, so the channel's
// pubDate is the same in a golden file as it is on the day the golden
// was written. In the blog's zone it is 2026-09-19 05:00 -0700.
var feedNow = time.Date(2026, time.September, 19, 12, 0, 0, 0, time.UTC)

// rssSite is the feed module on a clean database, with the same fixed
// blog zone the sitemap tests use and settings an operator would
// recognise.
type rssSite struct {
	*testSite
	rss *RSS
}

func newRSSSite(t *testing.T) *rssSite {
	t.Helper()
	st := testdb.New(t)
	cfg := &config.Config{BlogBaseURL: testBase, Port: 8080}
	settings := config.NewSettings(st, cfg)
	if err := settings.Reload(context.Background()); err != nil {
		t.Fatalf("settings reload: %v", err)
	}
	// A zone that is not UTC and whose offset moves with the seasons: an
	// item's pubDate and a permalink's date segments both follow it.
	if err := settings.Set(context.Background(), map[string]string{
		"timezone": "America/Los_Angeles",
		// An ampersand in the title and a `<` in the description: the feed
		// is XML, and both have to come back escaped exactly once.
		"blogtitle":       "Ray's Blog & Friends",
		"blogdescription": "Notes on <coldfusion> and café life",
		"locale":          "en_US",
		"owneremail":      "owner@example.com",
		"itunessubtitle":  "A podcast subtitle",
		"itunessummary":   "A podcast summary",
		"ituneskeywords":  "cfml,go,legacy",
		"itunesauthor":    "Ray Camden",
		"itunesimage":     "http://blog.example/images/cover.png",
		"itunesexplicit":  "no",
	}); err != nil {
		t.Fatalf("set settings: %v", err)
	}
	if settings.Timezone() == time.UTC {
		t.Skip("the America/Los_Angeles zone is not available here")
	}

	mux := http.NewServeMux()
	rss := NewRSS(cfg, st, settings)
	rss.now = func() time.Time { return feedNow }
	rss.Routes(mux)
	return &rssSite{
		testSite: &testSite{t: t, store: st, settings: settings, handler: mux},
		rss:      rss,
	}
}

// getWith is a conditional GET: the request headers an aggregator sends.
func (s *rssSite) getWith(path string, headers map[string]string) *httptest.ResponseRecorder {
	s.t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	return rec
}

// category creates one category for the fixtures.
func (s *rssSite) category(name, alias string) store.Category {
	s.t.Helper()
	c := store.Category{Name: name, Alias: alias}
	if err := s.store.CreateCategory(context.Background(), &c); err != nil {
		s.t.Fatalf("create category %q: %v", name, err)
	}
	return c
}

// inCategories files an entry under categories.
func (s *rssSite) inCategories(e store.Entry, cats ...store.Category) {
	s.t.Helper()
	ids := make([]string, 0, len(cats))
	for _, c := range cats {
		ids = append(ids, c.ID)
	}
	if err := s.store.SetEntryCategories(context.Background(), e.ID, ids); err != nil {
		s.t.Fatalf("set categories on %q: %v", e.Title, err)
	}
}

// feedBody asks for a feed, insists on 200 and a well-formed document,
// and returns it. Every feed a test reads goes through here, so the
// well-formedness check covers all of them, goldens included.
func (s *rssSite) feedBody(path string) string {
	s.t.Helper()
	rec := s.get(path)
	if rec.Code != http.StatusOK {
		s.t.Fatalf("GET %s = %d, want 200", path, rec.Code)
	}
	body := rec.Body.String()
	if err := xmlWellFormed(body); err != nil {
		s.t.Fatalf("GET %s is not well-formed XML: %v\n%s", path, err, body)
	}
	if !strings.HasPrefix(body, `<?xml version="1.0" encoding="utf-8"?>`) {
		s.t.Errorf("GET %s does not open with the UTF-8 prolog:\n%s", path, firstLine(body))
	}
	return body
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// countElements counts an element in a document by parsing it, not by
// counting substrings: `<category>` inside a body would fool the latter.
func countElements(t *testing.T, doc, name string) int {
	t.Helper()
	dec := xml.NewDecoder(strings.NewReader(doc))
	n := 0
	for {
		tok, err := dec.Token()
		if err != nil {
			return n
		}
		if start, ok := tok.(xml.StartElement); ok {
			full := start.Name.Local
			if strings.HasSuffix(name, ":"+full) || name == full {
				n++
			}
		}
	}
}

// TestFP_F01_RSS20ChannelAndItemFields: the RSS 2.0 document
// generateRSS wrote -- the channel's title (with the ampersand escaped),
// link, description, the language from the locale, pubDate,
// lastBuildDate from the newest entry, generator, managingEditor and
// webMaster, the iTunes block and the channel image -- and, per item,
// title, link, description, categories, pubDate, guid and author, with
// every URL built from BLOG_BASE_URL and every date in the blog's zone
// (PLAN §9 F01).
func TestFP_F01_RSS20ChannelAndItemFields(t *testing.T) {
	s := newRSSSite(t)
	cfml := s.category("ColdFusion & Friends", "coldfusion")
	go_ := s.category("Go", "go")

	// 2026-03-01 07:30Z is 2026-02-28 23:30 in PST: the permalink and the
	// pubDate are the blog zone's date, not the UTC one.
	winter := s.entry(store.Entry{Title: "Winter entry", Alias: "winter-entry", Released: true,
		Username: "admin", Body: "A short winter body with an & in it, and <em>markup</em>.",
		Posted: time.Date(2026, time.March, 1, 7, 30, 0, 0, time.UTC)})
	s.inCategories(winter, cfml, go_)

	// The newest entry, and so the channel's lastBuildDate.
	summer := s.entry(store.Entry{Title: "Summer & <b>markup</b> in a title", Alias: "summer-entry",
		Released: true, Username: "admin", Body: "Le café is open.",
		Posted: time.Date(2026, time.July, 4, 18, 0, 0, 0, time.UTC)})
	s.inCategories(summer, go_)

	// No alias: the id-form permalink, whose `&` must be escaped in XML.
	s.entry(store.Entry{ID: "11111111-2222-4333-8444-555555555555", Title: "No alias",
		Released: true, Username: "admin", Body: "Id form.",
		Posted: time.Date(2026, time.May, 5, 16, 0, 0, 0, time.UTC)})

	rec := s.get("/rss")
	if ct := rec.Header().Get("Content-Type"); ct != "application/rss+xml; charset=utf-8" {
		t.Errorf("RSS 2.0 Content-Type = %q", ct)
	}
	body := s.feedBody("/rss")

	for _, want := range []string{
		"<title>Ray&apos;s Blog &amp; Friends</title>",
		"<link>" + testBase + "</link>",
		"<description>Notes on &lt;coldfusion&gt; and café life</description>",
		"<language>en-us</language>",
		// The channel's pubDate is now in the blog's zone (PDT in September).
		"<pubDate>Sat, 19 Sep 2026 05:00:00 -0700</pubDate>",
		// lastBuildDate is the newest item.
		"<lastBuildDate>Sat, 04 Jul 2026 11:00:00 -0700</lastBuildDate>",
		"<generator>go-blogcfc</generator>",
		"<managingEditor>owner@example.com</managingEditor>",
		"<webMaster>owner@example.com</webMaster>",
		"<itunes:author>Ray Camden</itunes:author>",
		"<itunes:explicit>no</itunes:explicit>",
		"<url>http://blog.example/images/cover.png</url>",
		// Item fields.
		"<title>Summer &amp; &lt;b&gt;markup&lt;/b&gt; in a title</title>",
		"<link>" + testBase + "/2026/7/4/summer-entry</link>",
		"<guid isPermaLink=\"true\">" + testBase + "/2026/7/4/summer-entry</guid>",
		"<category>ColdFusion &amp; Friends</category>",
		"<pubDate>Sat, 28 Feb 2026 23:30:00 -0800</pubDate>",
		"<author>owner@example.com (Ray Camden)</author>",
		// The alias-less permalink's query `&`, escaped.
		"?mode=entry&amp;entry=11111111-2222-4333-8444-555555555555",
		// Non-ASCII rides through as UTF-8, unescaped.
		"Le café is open.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the feed is missing %q:\n%s", want, body)
		}
	}
	// Nothing is escaped twice.
	if strings.Contains(body, "&amp;amp;") || strings.Contains(body, "&amp;lt;") {
		t.Errorf("the feed escapes something twice:\n%s", body)
	}
	if n := countElements(t, body, "item"); n != 3 {
		t.Errorf("the feed has %d items, want 3", n)
	}
	checkGolden(t, "rss2.xml", body)
}

// TestFP_F02_ShortExcerptAndFullBody: `mode=short` sends the first 250
// characters of the tag-stripped body with an ellipsis, `mode=full`
// sends the rendered body and morebody, and -- generateRSS's own rule --
// a body too short to need excerpting is sent whole even in a short
// feed (PLAN §9 F02).
func TestFP_F02_ShortExcerptAndFullBody(t *testing.T) {
	s := newRSSSite(t)
	long := "<p>" + strings.Repeat("Alpha beta gamma delta. ", 20) + "</p>"
	s.entry(store.Entry{Title: "Long entry", Alias: "long-entry", Released: true, Username: "admin",
		Body: long, MoreBody: "<p>The rest of the story.</p>",
		Posted: time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)})

	short := s.feedBody("/rss")
	stripped := strings.Repeat("Alpha beta gamma delta. ", 20)
	want := stripped[:excerptChars] + "..."
	if !strings.Contains(short, want) {
		t.Errorf("the short feed does not carry the 250-character excerpt %q:\n%s", want, short)
	}
	if strings.Contains(short, "&lt;p&gt;") {
		t.Errorf("the short excerpt still carries markup:\n%s", short)
	}
	if strings.Contains(short, "The rest of the story") {
		t.Errorf("the short excerpt carries the morebody:\n%s", short)
	}

	full := s.feedBody("/rss?mode=full")
	if !strings.Contains(full, "&lt;p&gt;") {
		t.Errorf("the full feed does not carry the rendered body:\n%s", full)
	}
	if !strings.Contains(full, "The rest of the story.") {
		t.Errorf("the full feed does not carry the morebody:\n%s", full)
	}
	if strings.Contains(full, "...</description>") {
		t.Errorf("the full feed excerpted:\n%s", full)
	}

	// A body under the excerpt length is sent whole by both modes, which
	// is what generateRSS did: it only excerpted what was long enough.
	s.entry(store.Entry{Title: "Tiny entry", Alias: "tiny-entry", Released: true, Username: "admin",
		Body: "Two words.", Posted: time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)})
	short = s.feedBody("/rss")
	if !strings.Contains(short, "&lt;p&gt;Two words.&lt;/p&gt;") {
		t.Errorf("a short body was not sent whole in the short feed:\n%s", short)
	}
}

// TestFP_F03_CapAt15AndMode2Filters: at most 15 items whatever the blog
// holds, and the four mode2 filters -- day, month, cat (a comma list, as
// the as-is allowed) and entry -- each select what rss.cfm selected
// (PLAN §9 F03).
func TestFP_F03_CapAt15AndMode2Filters(t *testing.T) {
	s := newRSSSite(t)
	cat := s.category("Go", "go")
	other := s.category("CFML", "cfml")

	// 18 entries through June 2026, newest last.
	var june store.Entry
	for i := 1; i <= 18; i++ {
		e := s.entry(store.Entry{
			Title:    fmt.Sprintf("Entry %02d", i),
			Alias:    fmt.Sprintf("entry-%02d", i),
			Released: true, Username: "admin",
			Posted: time.Date(2026, time.June, i, 19, 0, 0, 0, time.UTC),
		})
		if i == 18 {
			june = e
		}
	}
	// One in another month, in a category, for the filters below.
	july := s.entry(store.Entry{Title: "July entry", Alias: "july-entry", Released: true,
		Username: "admin", Posted: time.Date(2026, time.July, 9, 20, 0, 0, 0, time.UTC)})
	s.inCategories(july, cat)
	s.inCategories(june, other)

	body := s.feedBody("/rss")
	if n := countElements(t, body, "item"); n != maxItems {
		t.Errorf("the unfiltered feed has %d items, want the cap of %d", n, maxItems)
	}
	if !strings.Contains(body, "<title>July entry</title>") {
		t.Errorf("the feed is not newest first:\n%s", body)
	}

	// mode2=day: 2026-06-18 19:00Z is 12:00 PDT on the 18th.
	day := s.feedBody("/rss?mode2=day&year=2026&month=6&day=18")
	if n := countElements(t, day, "item"); n != 1 {
		t.Errorf("the day feed has %d items, want 1:\n%s", n, day)
	}
	if !strings.Contains(day, "<title>Entry 18</title>") {
		t.Errorf("the day feed holds the wrong entry:\n%s", day)
	}

	// mode2=month: the 15-item cap still applies inside a month.
	month := s.feedBody("/rss?mode2=month&year=2026&month=6")
	if n := countElements(t, month, "item"); n != maxItems {
		t.Errorf("the month feed has %d items, want %d:\n%s", n, maxItems, month)
	}
	if strings.Contains(month, "July entry") {
		t.Errorf("the month feed reaches into July:\n%s", month)
	}
	if empty := s.feedBody("/rss?mode2=month&year=2025&month=1"); countElements(t, empty, "item") != 0 {
		t.Errorf("a month with no entries is not empty:\n%s", empty)
	}

	// mode2=cat, one id and then the comma list.
	one := s.feedBody("/rss?mode2=cat&catid=" + cat.ID)
	if n := countElements(t, one, "item"); n != 1 || !strings.Contains(one, "<title>July entry</title>") {
		t.Errorf("the category feed has %d items:\n%s", n, one)
	}
	if !strings.Contains(one, "<title>Ray&apos;s Blog &amp; Friends - Go</title>") {
		t.Errorf("the category feed does not carry the additional title:\n%s", one)
	}
	both := s.feedBody("/rss?mode2=cat&catid=" + cat.ID + "," + other.ID)
	if n := countElements(t, both, "item"); n != 2 {
		t.Errorf("the two-category feed has %d items, want 2:\n%s", n, both)
	}

	// mode2=entry, and an id that matches nothing.
	single := s.feedBody("/rss?mode2=entry&entry=" + july.ID)
	if n := countElements(t, single, "item"); n != 1 || !strings.Contains(single, "July entry") {
		t.Errorf("the entry feed has %d items:\n%s", n, single)
	}
	missing := s.feedBody("/rss?mode2=entry&entry=00000000-0000-4000-8000-000000000000")
	if n := countElements(t, missing, "item"); n != 0 {
		t.Errorf("an unknown entry id answered %d items:\n%s", n, missing)
	}

	// A mode2 whose parameters do not parse filters nothing, as the
	// as-is's val() checks did.
	junk := s.feedBody("/rss?mode2=day&year=banana&month=6&day=18")
	if n := countElements(t, junk, "item"); n != maxItems {
		t.Errorf("a nonsense day filter changed the feed (%d items):\n%s", n, junk)
	}
}

// TestFP_F04_EnclosureAndITunesTags: an entry with an attached file
// carries an enclosure with its url, length and type, and an audio/mpeg
// enclosure adds the iTunes tags generateRSS added -- author, explicit,
// duration, keywords, subtitle, summary and the cover image -- while
// any other type does not (PLAN §9 F04).
func TestFP_F04_EnclosureAndITunesTags(t *testing.T) {
	s := newRSSSite(t)
	s.entry(store.Entry{Title: "Podcast episode", Alias: "podcast-episode", Released: true,
		Username: "admin", Body: "Listen in.",
		Enclosure: "/var/lib/go-blogcfc/enclosures/episode 7.mp3", FileSize: 4823174,
		MimeType: "audio/mpeg", Duration: "00:31:12", Keywords: "cfml,go",
		Subtitle: "Episode seven", Summary: "Seven is the one about feeds",
		Posted: time.Date(2026, time.September, 10, 17, 0, 0, 0, time.UTC)})
	s.entry(store.Entry{Title: "Screenshot post", Alias: "screenshot-post", Released: true,
		Username: "admin", Body: "A picture.",
		Enclosure: "shot.png", FileSize: 1024, MimeType: "image/png",
		Posted: time.Date(2026, time.September, 11, 17, 0, 0, 0, time.UTC)})

	body := s.feedBody("/rss?mode=full")
	for _, want := range []string{
		`<enclosure url="` + testBase + `/enclosures/episode%207.mp3" length="4823174" type="audio/mpeg" />`,
		"<itunes:duration>00:31:12</itunes:duration>",
		"<itunes:subtitle>Episode seven</itunes:subtitle>",
		"<itunes:summary>Seven is the one about feeds</itunes:summary>",
		"<itunes:keywords>cfml,go</itunes:keywords>",
		`<enclosure url="` + testBase + `/enclosures/shot.png" length="1024" type="image/png" />`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the feed is missing %q:\n%s", want, body)
		}
	}
	// The image entry gets no podcast tags: only audio/mpeg earns them.
	if n := countElements(t, body, "itunes:duration"); n != 1 {
		t.Errorf("%d items carry itunes:duration, want the audio one only:\n%s", n, body)
	}
	checkGolden(t, "rss2_enclosure.xml", body)
}

// TestFP_F05_RSS10RDFShape: `version=1` answers the RDF document, with
// the channel's rdf:Seq listing every item, each item's rdf:about its
// permalink, and dc:date and dc:subject in place of pubDate and
// category. The as-is's dc:subject accumulated every earlier item's
// categories; this one does not (PLAN §9 F05).
func TestFP_F05_RSS10RDFShape(t *testing.T) {
	s := newRSSSite(t)
	cfml := s.category("ColdFusion & Friends", "coldfusion")
	go_ := s.category("Go", "go")

	first := s.entry(store.Entry{Title: "First", Alias: "first", Released: true, Username: "admin",
		Body: "First body.", Posted: time.Date(2026, time.March, 1, 7, 30, 0, 0, time.UTC)})
	s.inCategories(first, cfml)
	second := s.entry(store.Entry{Title: "Second", Alias: "second", Released: true, Username: "admin",
		Body: "Second body.", Posted: time.Date(2026, time.July, 4, 18, 0, 0, 0, time.UTC)})
	s.inCategories(second, go_)

	rec := s.get("/rss?version=1")
	if ct := rec.Header().Get("Content-Type"); ct != "application/rdf+xml; charset=utf-8" {
		t.Errorf("RSS 1.0 Content-Type = %q", ct)
	}
	body := s.feedBody("/rss?version=1")

	for _, want := range []string{
		`<rdf:RDF`,
		`xmlns="http://purl.org/rss/1.0/"`,
		`<channel rdf:about="` + testBase + `">`,
		`<rdf:li rdf:resource="` + testBase + `/2026/7/4/second" />`,
		`<item rdf:about="` + testBase + `/2026/2/28/first">`,
		// The W3C stamp, in the blog's zone on both sides of the DST change.
		"<dc:date>2026-07-04T11:00:00-07:00</dc:date>",
		"<dc:date>2026-02-28T23:30:00-08:00</dc:date>",
		"<dc:subject>Go</dc:subject>",
		"<dc:subject>ColdFusion &amp; Friends</dc:subject>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the RDF feed is missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "<pubDate>") || strings.Contains(body, "<category>") {
		t.Errorf("the RDF feed carries RSS 2.0 elements:\n%s", body)
	}
	if n := countElements(t, body, "rdf:li"); n != 2 {
		t.Errorf("the rdf:Seq lists %d items, want 2:\n%s", n, body)
	}
	// The second item's subject is its own category, not both.
	if strings.Contains(body, "<dc:subject>ColdFusion &amp; Friends,Go</dc:subject>") ||
		strings.Contains(body, "<dc:subject>Go,ColdFusion &amp; Friends</dc:subject>") {
		t.Errorf("dc:subject accumulated across items:\n%s", body)
	}
	checkGolden(t, "rss1.xml", body)
}

// TestFP_F06_ETagLastModifiedAnd304: the feed carries an ETag over its
// own bytes and a Last-Modified of the newest item, answers 304 with no
// body to a matching If-None-Match or a recent-enough If-Modified-Since,
// and answers 200 when either says something else. An empty blog is
// stamped now rather than failing, as generateRSS did (PLAN §9 F06).
func TestFP_F06_ETagLastModifiedAnd304(t *testing.T) {
	s := newRSSSite(t)
	posted := time.Date(2026, time.July, 4, 18, 0, 0, 0, time.UTC)
	only := s.entry(store.Entry{Title: "Only entry", Alias: "only-entry", Released: true, Username: "admin",
		Body: "Only.", Posted: posted})

	rec := s.get("/rss")
	etag := rec.Header().Get("ETag")
	if !strings.HasPrefix(etag, `"`) || !strings.HasSuffix(etag, `"`) || len(etag) < 10 {
		t.Fatalf("ETag = %q, want a quoted hash", etag)
	}
	if got, want := rec.Header().Get("Last-Modified"), posted.UTC().Format(http.TimeFormat); got != want {
		t.Errorf("Last-Modified = %q, want the newest item's posted %q", got, want)
	}

	// A second request is byte-identical, so the tag is stable.
	if again := s.get("/rss").Header().Get("ETag"); again != etag {
		t.Errorf("ETag changed between two identical requests: %q then %q", etag, again)
	}

	// If-None-Match: 304, no body, and the validators repeated.
	rec = s.getWith("/rss", map[string]string{"If-None-Match": etag})
	if rec.Code != http.StatusNotModified {
		t.Errorf("If-None-Match got %d, want 304", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("a 304 carried %d bytes of body", rec.Body.Len())
	}
	if rec.Header().Get("ETag") != etag {
		t.Errorf("the 304 dropped the ETag")
	}
	// A weak form of the same tag still matches.
	if rec := s.getWith("/rss", map[string]string{"If-None-Match": "W/" + etag}); rec.Code != http.StatusNotModified {
		t.Errorf("a weak If-None-Match got %d, want 304", rec.Code)
	}
	// Somebody else's tag does not.
	if rec := s.getWith("/rss", map[string]string{"If-None-Match": `"nonsense"`}); rec.Code != http.StatusOK {
		t.Errorf("a stale If-None-Match got %d, want 200", rec.Code)
	}

	// If-Modified-Since on its own.
	since := posted.UTC().Format(http.TimeFormat)
	if rec := s.getWith("/rss", map[string]string{"If-Modified-Since": since}); rec.Code != http.StatusNotModified {
		t.Errorf("If-Modified-Since at the item's time got %d, want 304", rec.Code)
	}
	older := posted.Add(-time.Hour).UTC().Format(http.TimeFormat)
	if rec := s.getWith("/rss", map[string]string{"If-Modified-Since": older}); rec.Code != http.StatusOK {
		t.Errorf("an older If-Modified-Since got %d, want 200", rec.Code)
	}

	// A new entry changes both the tag and the stamp.
	newer := time.Date(2026, time.August, 9, 18, 0, 0, 0, time.UTC)
	newest := s.entry(store.Entry{Title: "Newer entry", Alias: "newer-entry", Released: true, Username: "admin",
		Body: "Newer.", Posted: newer})
	rec = s.get("/rss")
	if rec.Header().Get("ETag") == etag {
		t.Errorf("the ETag survived a new entry: %q", etag)
	}
	if got, want := rec.Header().Get("Last-Modified"), newer.UTC().Format(http.TimeFormat); got != want {
		t.Errorf("Last-Modified = %q, want %q", got, want)
	}

	// A blog with nothing live in it answers a feed stamped now rather
	// than failing, which is where generateRSS blew up: it read
	// `articles.posted[1]` with no articles.
	if err := s.store.DeleteEntries(context.Background(), []string{only.ID, newest.ID}); err != nil {
		t.Fatalf("empty the blog: %v", err)
	}
	rec = s.get("/rss")
	if rec.Code != http.StatusOK {
		t.Fatalf("an empty blog's feed = %d, want 200", rec.Code)
	}
	if got, want := rec.Header().Get("Last-Modified"), feedNow.UTC().Format(http.TimeFormat); got != want {
		t.Errorf("an empty feed's Last-Modified = %q, want now (%q)", got, want)
	}
	body := rec.Body.String()
	if err := xmlWellFormed(body); err != nil {
		t.Errorf("an empty feed does not parse: %v\n%s", err, body)
	}
	if n := countElements(t, body, "item"); n != 0 {
		t.Errorf("an empty blog's feed has %d items:\n%s", n, body)
	}
}

// TestFP_F07_OnlyLiveEntries: a draft and a scheduled entry are in no
// feed -- not the plain one, not a filtered one, and not even when the
// feed is asked for that entry by id (PLAN §9 F07, §11 "Three entry
// states").
func TestFP_F07_OnlyLiveEntries(t *testing.T) {
	s := newRSSSite(t)
	cat := s.category("Go", "go")

	live := s.entry(store.Entry{Title: "Live entry", Alias: "live-entry", Released: true,
		Username: "admin", Body: "Live.",
		Posted: time.Date(2026, time.September, 1, 17, 0, 0, 0, time.UTC)})
	s.inCategories(live, cat)
	draft := s.entry(store.Entry{Title: "Draft entry", Alias: "draft-entry", Username: "admin",
		Body: "Draft.", Posted: time.Date(2026, time.September, 2, 17, 0, 0, 0, time.UTC)})
	s.inCategories(draft, cat)
	scheduled := s.entry(store.Entry{Title: "Scheduled entry", Alias: "scheduled-entry", Released: true,
		Username: "admin", Body: "Scheduled.", Posted: feedNow.Add(72 * time.Hour)})
	s.inCategories(scheduled, cat)

	for _, path := range []string{
		"/rss",
		"/rss?mode=full",
		"/rss?version=1",
		"/rss?mode2=cat&catid=" + cat.ID,
		"/rss?mode2=month&year=2026&month=9",
	} {
		body := s.feedBody(path)
		for _, absent := range []string{"Draft entry", "Scheduled entry"} {
			if strings.Contains(body, absent) {
				t.Errorf("GET %s lists %q, which is not live:\n%s", path, absent, body)
			}
		}
		if !strings.Contains(body, "Live entry") {
			t.Errorf("GET %s dropped the live entry:\n%s", path, body)
		}
	}

	// Asked for by id, a draft and a scheduled entry are still nothing.
	for _, e := range []store.Entry{draft, scheduled} {
		body := s.feedBody("/rss?mode2=entry&entry=" + e.ID)
		if n := countElements(t, body, "item"); n != 0 {
			t.Errorf("mode2=entry served %q, which is not live:\n%s", e.Title, body)
		}
	}
}

// TestRSSGoldensAreWellFormed parses every golden feed in testdata, so a
// golden updated with -update cannot quietly become invalid XML.
func TestRSSGoldensAreWellFormed(t *testing.T) {
	for _, name := range []string{"rss2.xml", "rss2_enclosure.xml", "rss1.xml", "sitemap.xml"} {
		b, err := readGolden(name)
		if err != nil {
			t.Fatalf("golden %s: %v (run `go test ./internal/feeds/ -update`)", name, err)
		}
		if err := xmlWellFormed(string(b)); err != nil {
			t.Errorf("golden %s does not parse: %v", name, err)
		}
	}
}

// readGolden reads one golden file from testdata.
func readGolden(name string) ([]byte, error) {
	return os.ReadFile(filepath.Join("testdata", name))
}

// TestRSSCachesOnlyTheUnfilteredFeed: with a cache wired in, the plain
// feed is held per mode and version, as rss.cfm's scopecache held it
// under `<appname>_rss_<mode><version>`, and a mode2 request is never
// cached. A flush -- the admin's, or ?reinit=1 -- brings the new entry
// back (PLAN §11 "Caching", §9 A29).
func TestRSSCachesOnlyTheUnfilteredFeed(t *testing.T) {
	s := newRSSSite(t)
	s.rss.Cache = cache.New()
	s.entry(store.Entry{Title: "First post", Alias: "first-post", Released: true, Username: "admin",
		Body: "First.", Posted: time.Date(2026, time.September, 1, 17, 0, 0, 0, time.UTC)})

	before := s.feedBody("/rss")
	s.entry(store.Entry{Title: "Second post", Alias: "second-post", Released: true, Username: "admin",
		Body: "Second.", Posted: time.Date(2026, time.September, 2, 17, 0, 0, 0, time.UTC)})

	if again := s.feedBody("/rss"); again != before {
		t.Errorf("the plain feed was not cached:\n%s", again)
	}
	// Another mode, another version and a filtered feed are all their own
	// answers, and the filtered one is not cached at all.
	for _, path := range []string{"/rss?mode=full", "/rss?version=1", "/rss?mode2=month&year=2026&month=9"} {
		if body := s.feedBody(path); !strings.Contains(body, "Second post") {
			t.Errorf("GET %s served the plain feed's cached answer:\n%s", path, body)
		}
	}
	s.rss.Cache.Flush()
	if after := s.feedBody("/rss"); !strings.Contains(after, "Second post") {
		t.Errorf("the feed was still cached after a flush:\n%s", after)
	}
}
