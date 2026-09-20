package web

import (
	"context"
	"crypto/md5"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/4scottt/go-blogcfc/internal/store"
)

// seedEntries inserts n released, past-dated entries, newest last, and
// returns them in the order they were created.
func (s *testSite) seedEntries(n int, username string) []store.Entry {
	s.t.Helper()
	start := utc(2026, 3, 1, 12, 0)
	out := make([]store.Entry, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, s.entry(store.Entry{
			Title:    fmt.Sprintf("Entry %02d", i),
			Alias:    fmt.Sprintf("entry-%02d", i),
			Posted:   start.AddDate(0, 0, i),
			Username: username,
			Released: true,
		}))
	}
	return out
}

// TestFP_P01_HomeListsNewestReleasedNonFutureNewestFirst is P01: the home
// page shows the newest `maxentries` live entries, newest first.
func TestFP_P01_HomeListsNewestReleasedNonFutureNewestFirst(t *testing.T) {
	s := newTestSite(t, nil)
	s.user("ray", "Raymond Camden")
	s.seedEntries(12, "ray")
	s.entry(store.Entry{Title: "Draft", Alias: "draft", Posted: utc(2026, 4, 1, 9, 0), Username: "ray"})
	s.entry(store.Entry{Title: "Future", Alias: "future", Posted: time.Now().UTC().Add(72 * time.Hour), Username: "ray", Released: true})

	body := s.getOK("/")

	if n := countPosts(body); n != 10 {
		t.Errorf("home listed %d entries, want maxentries = 10", n)
	}
	want := []string{"Entry 12", "Entry 11", "Entry 10", "Entry 09", "Entry 08",
		"Entry 07", "Entry 06", "Entry 05", "Entry 04", "Entry 03"}
	got := postTitles(body)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("home order = %v, want %v", got, want)
	}
	for _, hidden := range []string{"Draft", "Future"} {
		if strings.Contains(body, ">"+hidden+"<") {
			t.Errorf("home shows %s", hidden)
		}
	}

	// maxentries is a setting, not a constant.
	s.setSetting("maxentries", "3")
	if n := countPosts(s.getOK("/")); n != 3 {
		t.Errorf("home listed %d entries after maxentries=3", n)
	}
}

// TestFP_P02_DraftsAndFutureHiddenAndAdminviewShowsDrafts is P02: the
// three entry states, and getmode.cfm's rule that only a logged-in admin
// asking for ?adminview=1 sees the unreleased ones.
func TestFP_P02_DraftsAndFutureHiddenAndAdminviewShowsDrafts(t *testing.T) {
	seed := func(s *testSite) {
		s.user("ray", "Raymond Camden")
		s.entry(store.Entry{Title: "Live", Alias: "live", Posted: utc(2026, 3, 2, 12, 0), Username: "ray", Released: true})
		s.entry(store.Entry{Title: "Draft", Alias: "draft", Posted: utc(2026, 3, 3, 12, 0), Username: "ray"})
		s.entry(store.Entry{Title: "Future", Alias: "future", Posted: time.Now().UTC().Add(48 * time.Hour), Username: "ray", Released: true})
	}

	t.Run("nobody logged in", func(t *testing.T) {
		s := newTestSite(t, nil)
		seed(s)
		for _, path := range []string{"/", "/?adminview=1"} {
			body := s.getOK(path)
			if !strings.Contains(body, ">Live<") {
				t.Errorf("GET %s hides the live entry", path)
			}
			for _, hidden := range []string{"Draft", "Future"} {
				if strings.Contains(body, ">"+hidden+"<") {
					t.Errorf("GET %s shows %s to an anonymous visitor", path, hidden)
				}
			}
		}
		// A draft's permalink is a 404 for a visitor.
		if code := s.get("/2026/3/3/draft").Code; code != http.StatusNotFound {
			t.Errorf("draft permalink = %d, want 404", code)
		}
	})

	t.Run("logged in", func(t *testing.T) {
		s := newTestSite(t, fakeIdentity{user: &store.User{Username: "ray", Name: "Raymond Camden"}})
		seed(s)

		// Without adminview the logged-in admin sees the public blog.
		plain := s.getOK("/")
		if strings.Contains(plain, ">Draft<") {
			t.Error("the home page shows a draft without ?adminview=1")
		}

		// With it, BlogCFC drops the released-only filter entirely, so the
		// scheduled entry shows up beside the draft.
		admin := s.getOK("/?adminview=1")
		for _, shown := range []string{"Live", "Draft", "Future"} {
			if !strings.Contains(admin, ">"+shown+"<") {
				t.Errorf("?adminview=1 hides %s from an admin", shown)
			}
		}
		if code := s.get("/2026/3/3/draft?adminview=1").Code; code != http.StatusOK {
			t.Errorf("draft permalink with adminview = %d, want 200", code)
		}
	})
}

// TestFP_P03_PaginationStartRowKeepsPathAndQuery is P03: BlogCFC's 1-based
// startRow cursor, with links that keep the SES path and the rest of the
// query.
func TestFP_P03_PaginationStartRowKeepsPathAndQuery(t *testing.T) {
	s := newTestSite(t, nil)
	s.user("ray", "Raymond Camden")
	entries := s.seedEntries(25, "ray")
	cat := s.category("11111111-1111-4111-8111-111111111111", "ColdFusion", "coldfusion")
	for _, e := range entries {
		s.categorise(e.ID, cat.ID)
	}

	first := s.getOK("/")
	if strings.Contains(first, "Previous Entries") {
		t.Error("the first page offers a previous link")
	}
	if !strings.Contains(first, `<a href="`+testBase+`/?startRow=11">More Entries</a>`) {
		t.Errorf("the first page's next link is wrong:\n%s", pagerBlock.FindString(first))
	}

	second := s.getOK("/?startRow=11")
	if got := postTitles(second); got[0] != "Entry 15" || len(got) != 10 {
		t.Errorf("second page = %v, want ten entries starting at Entry 15", got)
	}
	if !strings.Contains(second, `<a href="`+testBase+`/">Previous Entries</a>`) {
		t.Errorf("the second page's previous link is wrong:\n%s", pagerBlock.FindString(second))
	}
	if !strings.Contains(second, `<a href="`+testBase+`/?startRow=21">More Entries</a>`) {
		t.Errorf("the second page's next link is wrong:\n%s", pagerBlock.FindString(second))
	}
	checkGolden(t, "pager_second_page.html", pagerBlock.FindString(second))

	last := s.getOK("/?startRow=21")
	if n := countPosts(last); n != 5 {
		t.Errorf("last page has %d entries, want 5", n)
	}
	if strings.Contains(last, "More Entries") {
		t.Error("the last page offers a next link")
	}

	// The SES path and every other query parameter survive.
	onCategory := s.getOK("/coldfusion?foo=bar&startRow=11")
	for _, want := range []string{
		`<a href="` + testBase + `/coldfusion?foo=bar">Previous Entries</a>`,
		`<a href="` + testBase + `/coldfusion?foo=bar&amp;startRow=21">More Entries</a>`,
	} {
		if !strings.Contains(onCategory, want) {
			t.Errorf("category pager is missing %q:\n%s", want, pagerBlock.FindString(onCategory))
		}
	}

	// A nonsense cursor falls back to row 1, as getmode.cfm does.
	for _, bad := range []string{"/?startRow=0", "/?startRow=-4", "/?startRow=abc"} {
		if got := postTitles(s.getOK(bad)); got[0] != "Entry 25" {
			t.Errorf("GET %s started at %q, want Entry 25", bad, got[0])
		}
	}
}

// TestFP_P04_EntryByDatePathAndByModeEntryAnd404 is P04: the SES permalink,
// the id fallback and the 404s.
func TestFP_P04_EntryByDatePathAndByModeEntryAnd404(t *testing.T) {
	s := newTestSite(t, nil)
	s.user("ray", "Raymond Camden")
	s.entry(store.Entry{
		ID: "22222222-2222-4222-8222-222222222222", Title: "Hello World", Alias: "Hello-World",
		Body: "<p>the body</p>", Posted: utc(2026, 3, 5, 14, 30), Username: "ray", Released: true,
	})
	noAlias := s.entry(store.Entry{
		ID: "33333333-3333-4333-8333-333333333333", Title: "No Alias",
		Body: "<p>alias-less</p>", Posted: utc(2026, 3, 6, 9, 0), Username: "ray", Released: true,
	})

	body := s.getOK("/2026/3/5/Hello-World")
	if !strings.Contains(body, "<p>the body</p>") {
		t.Error("the entry page does not show the body")
	}
	if !strings.Contains(body, "<title>BlogCFC - Hello World</title>") {
		t.Error("the entry page's title does not carry the entry title")
	}
	if n := countPosts(body); n != 1 {
		t.Errorf("the entry page rendered %d posts, want 1", n)
	}

	byID := s.getOK("/?mode=entry&entry=" + noAlias.ID)
	if !strings.Contains(byID, "<p>alias-less</p>") {
		t.Error("?mode=entry did not render the entry")
	}

	// BlogCFC's SES query carries the date beside the alias, so the alias
	// under another date finds nothing.
	for _, path := range []string{
		"/2026/3/6/Hello-World",   // right alias, wrong day
		"/2026/4/5/Hello-World",   // wrong month
		"/2025/3/5/Hello-World",   // wrong year
		"/2026/3/5/no-such-alias", // unknown alias
		"/2026/13/5/Hello-World",  // month out of range
		"/20x6/3/5/Hello-World",   // not numeric
		"/?mode=entry&entry=44444444-4444-4444-8444-444444444444",
	} {
		if code := s.get(path).Code; code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, code)
		}
	}

	// The 404 page is a plain page inside the layout.
	rec := s.get("/2026/3/5/no-such-alias")
	if !strings.Contains(rec.Body.String(), `<div id="main-content">`) {
		t.Error("the 404 page is not rendered in the layout")
	}
}

// TestFP_P06_CategoryByAliasAndByCatidList is P06.
func TestFP_P06_CategoryByAliasAndByCatidList(t *testing.T) {
	s := newTestSite(t, nil)
	s.user("ray", "Raymond Camden")
	cf := s.category("11111111-1111-4111-8111-111111111111", "ColdFusion", "coldfusion")
	go2 := s.category("55555555-5555-4555-8555-555555555555", "Go", "go")

	inCF := s.entry(store.Entry{Title: "CF Entry", Alias: "cf-entry", Posted: utc(2026, 3, 2, 12, 0), Username: "ray", Released: true})
	inGo := s.entry(store.Entry{Title: "Go Entry", Alias: "go-entry", Posted: utc(2026, 3, 3, 12, 0), Username: "ray", Released: true})
	s.entry(store.Entry{Title: "Uncategorised", Alias: "uncat", Posted: utc(2026, 3, 4, 12, 0), Username: "ray", Released: true})
	s.categorise(inCF.ID, cf.ID)
	s.categorise(inGo.ID, go2.ID)

	byAlias := s.getOK("/coldfusion")
	if got := postTitles(byAlias); len(got) != 1 || got[0] != "CF Entry" {
		t.Errorf("/coldfusion listed %v, want just the CF entry", got)
	}
	if !strings.Contains(byAlias, "<title>BlogCFC - ColdFusion</title>") {
		t.Error("the category page's title does not name the category")
	}
	// The entry header links back to the category by its alias.
	if !strings.Contains(byAlias, `<a href="`+testBase+`/coldfusion">ColdFusion</a>`) {
		t.Error("the entry header is missing the category link")
	}

	// A trailing slash is the same URL: CFML's list functions ignore the
	// empty element, and the catch-all route does the same.
	if got := postTitles(s.getOK("/coldfusion/")); len(got) != 1 || got[0] != "CF Entry" {
		t.Errorf("/coldfusion/ listed %v, want just the CF entry", got)
	}

	list := s.getOK("/?mode=cat&catid=" + cf.ID + "," + go2.ID)
	if got := postTitles(list); len(got) != 2 || got[0] != "Go Entry" || got[1] != "CF Entry" {
		t.Errorf("the catid list showed %v, want both entries newest first", got)
	}
	if !strings.Contains(list, "<title>BlogCFC - ColdFusion, Go</title>") {
		t.Error("the catid list's title does not name both categories")
	}

	// An unknown id in the list is ignored; a list of only unknown ids is
	// a 404, as is an unknown alias and a reserved path.
	mixed := s.getOK("/?mode=cat&catid=" + cf.ID + ",66666666-6666-4666-8666-666666666666")
	if got := postTitles(mixed); len(got) != 1 || got[0] != "CF Entry" {
		t.Errorf("a mixed catid list showed %v", got)
	}
	// The reserved segments here are the ones no route in this module
	// claims yet; /search, /contact and the rest answer for themselves as
	// their packages land, and none of them is ever a category lookup.
	for _, path := range []string{
		"/?mode=cat&catid=66666666-6666-4666-8666-666666666666",
		"/no-such-category",
		"/rss", "/admin", "/sitemap.xml", "/robots.txt",
	} {
		if code := s.get(path).Code; code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, code)
		}
	}

	// An empty category is the "no entries for your criteria" state, 200.
	s.category("77777777-7777-4777-8777-777777777777", "Empty", "empty")
	emptyBody := s.getOK("/empty")
	if !strings.Contains(emptyBody, "There are no blog entries available that match your criteria.") {
		t.Error("an empty category does not use the noentriesforcriteria string")
	}
}

// TestFP_P07_MonthAndDayArchives is P07: the month and day archives, with
// their bounds read in the blog's zone.
func TestFP_P07_MonthAndDayArchives(t *testing.T) {
	s := newTestSite(t, nil)
	s.user("ray", "Raymond Camden")
	s.entry(store.Entry{Title: "March Two", Alias: "march-two", Posted: utc(2026, 3, 2, 12, 0), Username: "ray", Released: true})
	s.entry(store.Entry{Title: "March Ten", Alias: "march-ten", Posted: utc(2026, 3, 10, 8, 0), Username: "ray", Released: true})
	s.entry(store.Entry{Title: "April One", Alias: "april-one", Posted: utc(2026, 4, 1, 8, 0), Username: "ray", Released: true})
	// 2026-03-01T02:00Z is still 28 February in Los Angeles.
	s.entry(store.Entry{Title: "Zone Edge", Alias: "zone-edge", Posted: utc(2026, 3, 1, 2, 0), Username: "ray", Released: true})

	month := s.getOK("/2026/3")
	if got, want := postTitles(month), []string{"March Ten", "March Two", "Zone Edge"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("/2026/3 listed %v, want %v", got, want)
	}
	if !strings.Contains(month, "<title>BlogCFC - Archives</title>") {
		t.Error("an archive page's title does not say Archives")
	}

	day := s.getOK("/2026/3/10")
	if got := postTitles(day); len(got) != 1 || got[0] != "March Ten" {
		t.Errorf("/2026/3/10 listed %v", got)
	}

	emptyDay := s.getOK("/2026/3/11")
	if !strings.Contains(emptyDay, "There are no blog entries available that match your criteria.") {
		t.Error("an empty day archive does not use the noentriesforcriteria string")
	}

	// In the blog's zone the edge entry belongs to February.
	s.setSetting("timezone", "America/Los_Angeles")
	if got := postTitles(s.getOK("/2026/2")); len(got) != 1 || got[0] != "Zone Edge" {
		t.Errorf("/2026/2 in America/Los_Angeles listed %v, want the edge entry", got)
	}
	if got := postTitles(s.getOK("/2026/2/28")); len(got) != 1 || got[0] != "Zone Edge" {
		t.Errorf("/2026/2/28 in America/Los_Angeles listed %v", got)
	}
	if got := postTitles(s.getOK("/2026/3")); len(got) != 2 {
		t.Errorf("/2026/3 in America/Los_Angeles listed %v, want the two March entries", got)
	}

	// More than four segments is no SES URL at all.
	for _, path := range []string{"/2026/0", "/2026/13", "/2026/3/0", "/2026/3/32", "/notayear/3", "/2026/3/10/alias/extra"} {
		if code := s.get(path).Code; code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, code)
		}
	}
}

// TestFP_P08_PostedByListing is P08.
func TestFP_P08_PostedByListing(t *testing.T) {
	s := newTestSite(t, nil)
	s.user("ray", "Raymond Camden")
	s.user("scott", "Scott Taylor")
	s.entry(store.Entry{Title: "By Ray", Alias: "by-ray", Posted: utc(2026, 3, 2, 12, 0), Username: "ray", Released: true})
	s.entry(store.Entry{Title: "By Scott", Alias: "by-scott", Posted: utc(2026, 3, 3, 12, 0), Username: "scott", Released: true})

	body := s.getOK("/postedby/ray")
	if got := postTitles(body); len(got) != 1 || got[0] != "By Ray" {
		t.Errorf("/postedby/ray listed %v", got)
	}
	if !strings.Contains(body, "<title>BlogCFC - Posted By Raymond Camden</title>") {
		t.Error("the posted-by page's title does not name the author")
	}
	// The entry header links to the author's listing.
	if !strings.Contains(body, `<a href="`+testBase+`/postedby/ray">ray</a>`) {
		t.Error("the entry header is missing the author link")
	}
	if code := s.get("/postedby/nobody").Code; code != http.StatusNotFound {
		t.Error("an unknown author is not a 404")
	}
	// The same listing through the catch-all, with a trailing slash.
	if got := postTitles(s.getOK("/postedby/ray/")); len(got) != 1 || got[0] != "By Ray" {
		t.Errorf("/postedby/ray/ listed %v", got)
	}
}

// TestFP_P09_MoreLinkInListsFullBodyOnEntry is P09: a list view shows the
// body and a [more] link; the entry view shows body and morebody.
func TestFP_P09_MoreLinkInListsFullBodyOnEntry(t *testing.T) {
	s := newTestSite(t, nil)
	s.user("ray", "Raymond Camden")
	cat := s.category("11111111-1111-4111-8111-111111111111", "ColdFusion", "coldfusion")
	e := s.entry(store.Entry{
		ID: "88888888-8888-4888-8888-888888888888", Title: "Split Entry", Alias: "split-entry",
		Body: "The first half.", MoreBody: "The second half.",
		Posted: utc(2026, 3, 5, 14, 30), Username: "ray", Released: true, Views: 42,
	})
	s.categorise(e.ID, cat.ID)
	permalink := testBase + "/2026/3/5/split-entry"

	// The paragraphs are render.Entry's: the editor stores what the
	// author typed and the pipeline wraps it (PLAN §9 R03).
	list := s.getOK("/")
	if !strings.Contains(list, "<p>The first half.</p>") {
		t.Error("the list view does not show the body")
	}
	if strings.Contains(list, "The second half.") {
		t.Error("the list view shows morebody")
	}
	if !strings.Contains(list, `<a href="`+permalink+`#more">[More]</a>`) {
		t.Errorf("the list view is missing the [more] link:\n%s", postBlock.FindString(list))
	}

	entry := s.getOK("/2026/3/5/split-entry")
	if !strings.Contains(entry, "<p>The first half.</p>") || !strings.Contains(entry, "<p>The second half.</p>") {
		t.Error("the entry view does not show both halves")
	}
	if strings.Contains(entry, ">[More]<") {
		t.Error("the entry view still offers a [more] link")
	}
	checkGolden(t, "entry_block.html", postBlock.FindString(entry))

	// An entry without a morebody gets no link at all.
	s.entry(store.Entry{Title: "Whole", Alias: "whole", Body: "All of it.",
		Posted: utc(2026, 3, 6, 9, 0), Username: "ray", Released: true})
	if n := strings.Count(s.getOK("/"), ">[More]<"); n != 1 {
		t.Errorf("the home page has %d [more] links, want 1", n)
	}
}

// TestFP_P12_ViewsCountedOncePerVisitorNeverOnPrint is P12: the single
// entry view adds one view per visitor per entry -- BlogCFC's
// session.viewedpages, here a signed cookie -- and no other view counts.
func TestFP_P12_ViewsCountedOncePerVisitorNeverOnPrint(t *testing.T) {
	s := newTestSite(t, nil)
	s.user("ray", "Raymond Camden")
	e := s.entry(store.Entry{
		ID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Title: "Counted", Alias: "counted",
		Posted: utc(2026, 5, 1, 9, 0), Username: "ray", Released: true,
	})
	permalink := "/2026/5/1/counted"

	// A listing never counts.
	s.getOK("/")
	if got := s.views(e.ID); got != 0 {
		t.Fatalf("the home page counted %d views, want 0", got)
	}

	first := s.visitor()
	if rec := first.get(permalink); rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d", permalink, rec.Code)
	}
	if got := s.views(e.ID); got != 1 {
		t.Fatalf("after one visit views = %d, want 1", got)
	}

	// The same visitor, twice more, by both URL forms: still one.
	first.get(permalink)
	first.get("/?mode=entry&entry=" + e.ID)
	if got := s.views(e.ID); got != 1 {
		t.Errorf("the same visitor counted %d views, want 1", got)
	}

	// A second browser is a second visitor.
	second := s.visitor()
	second.get(permalink)
	if got := s.views(e.ID); got != 2 {
		t.Errorf("after a second visitor views = %d, want 2", got)
	}

	// The print view never counts, however often it is asked for.
	third := s.visitor()
	if rec := third.get("/print/" + e.ID); rec.Code != http.StatusOK {
		t.Fatalf("GET /print/%s = %d", e.ID, rec.Code)
	}
	third.get("/print/" + e.ID)
	if got := s.views(e.ID); got != 2 {
		t.Errorf("the print view counted: views = %d, want 2", got)
	}

	// A forged cookie is not a visitor who has been here: the signature
	// fails, the set is read as empty, and the view counts.
	forger := s.visitor()
	forger.cookies = []*http.Cookie{{Name: seenCookie, Value: e.ID + "|deadbeef"}}
	forger.get(permalink)
	if got := s.views(e.ID); got != 3 {
		t.Errorf("a tampered cookie suppressed the count: views = %d, want 3", got)
	}

	// The footer prints the count this visit found, as index.cfm did:
	// the read happens before the increment. This visitor has already
	// been counted, so the page only reports.
	meta := metadataBlock.FindString(first.get(permalink).Body.String())
	if !strings.Contains(meta, "has received 3 views") {
		t.Errorf("the footer does not show the view count:\n%s", meta)
	}

	// The set is capped: a visitor who has read seenMax other entries
	// counts this one and drops the oldest id off the front, so the
	// cookie never grows without bound (PLAN §11, "Views").
	full := make([]string, seenMax)
	for i := range full {
		full[i] = fmt.Sprintf("old-entry-%03d", i)
	}
	capped := s.visitor()
	capped.cookies = []*http.Cookie{s.seenCookie(full)}
	capped.get(permalink)
	if got := s.views(e.ID); got != 4 {
		t.Errorf("a full cookie suppressed the count: views = %d, want 4", got)
	}
	after := s.module.readSeen(cookieRequest(capped.cookies))
	if len(after) != seenMax {
		t.Errorf("the seen set holds %d ids, want the cap of %d", len(after), seenMax)
	}
	if len(after) == seenMax && (after[0] != full[1] || after[seenMax-1] != e.ID) {
		t.Errorf("the cap dropped the wrong end: first %q, last %q", after[0], after[seenMax-1])
	}
}

// TestFP_P13_RelatedEntriesBidirectionalLiveOnly is P13: the related
// entries block reads the link in both directions, shows live entries
// only, and is absent when nothing is related.
func TestFP_P13_RelatedEntriesBidirectionalLiveOnly(t *testing.T) {
	s := newTestSite(t, nil)
	s.user("ray", "Raymond Camden")
	one := s.entry(store.Entry{
		ID: "b1111111-1111-4111-8111-111111111111", Title: "One", Alias: "one",
		Posted: utc(2026, 5, 2, 9, 0), Username: "ray", Released: true,
	})
	two := s.entry(store.Entry{
		ID: "b2222222-2222-4222-8222-222222222222", Title: "Two", Alias: "two",
		Posted: utc(2026, 5, 3, 9, 0), Username: "ray", Released: true,
	})
	draft := s.entry(store.Entry{
		ID: "b3333333-3333-4333-8333-333333333333", Title: "Draft", Alias: "draft",
		Posted: utc(2026, 5, 4, 9, 0), Username: "ray", Released: false,
	})
	future := s.entry(store.Entry{
		ID: "b4444444-4444-4444-8444-444444444444", Title: "Future", Alias: "future",
		Posted: time.Now().UTC().AddDate(0, 0, 7), Username: "ray", Released: true,
	})
	lonely := s.entry(store.Entry{
		ID: "b5555555-5555-4555-8555-555555555555", Title: "Lonely", Alias: "lonely",
		Posted: utc(2026, 5, 5, 9, 0), Username: "ray", Released: true,
	})
	// One names the other three; nobody names One.
	s.relate(one.ID, two.ID, draft.ID, future.ID)

	// The entry that does the naming sees the live one only.
	block := relatedBlock.FindString(s.getOK("/2026/5/2/one"))
	if block == "" {
		t.Fatal("no related entries block on the naming entry")
	}
	if !strings.Contains(block, `<div class="relatedentriesHeader">Related Blog Entries</div>`) {
		t.Errorf("the related block has no header:\n%s", block)
	}
	if !strings.Contains(block, `<a href="`+testBase+`/2026/5/3/two">Two</a>`) {
		t.Errorf("the related block does not link the related entry:\n%s", block)
	}
	for _, hidden := range []string{"Draft", "Future"} {
		if strings.Contains(block, hidden) {
			t.Errorf("the related block shows %s, which is not live:\n%s", hidden, block)
		}
	}

	// The entry that was named sees it too: the link reads both ways.
	back := relatedBlock.FindString(s.getOK("/2026/5/3/two"))
	if !strings.Contains(back, `<a href="`+testBase+`/2026/5/2/one">One</a>`) {
		t.Errorf("the related block is not bidirectional:\n%s", back)
	}

	// Nothing related, no block at all.
	if got := relatedBlock.FindString(s.getOK("/2026/5/5/lonely")); got != "" {
		t.Errorf("an entry with nothing related still has a block:\n%s", got)
	}
	_ = lonely
}

// TestFP_P14_CommentsListGravatarParagraphsLinks is P14: the comment list
// under an entry -- the anchor and heading, one li.comment per moderated
// comment with its Gravatar, the "said on" line, and a body whose line
// breaks survive, whose URLs are links and whose markup is escaped.
func TestFP_P14_CommentsListGravatarParagraphsLinks(t *testing.T) {
	s := newTestSite(t, nil)
	s.user("ray", "Raymond Camden")
	e := s.entry(store.Entry{
		ID: "c0000000-0000-4000-8000-000000000000", Title: "Talkative", Alias: "talkative",
		Posted: utc(2026, 5, 6, 9, 0), Username: "ray", Released: true, AllowComments: true,
	})
	s.comment(store.Comment{
		ID: "c1111111-1111-4111-8111-111111111111", EntryID: e.ID,
		Name: "Pete F", Email: "Pete@Example.COM", Website: "http://pete.example/blog",
		Comment:   "First line.\nSecond line.\n\nSee http://www.coldfusionjedi.com/index.cfm for more.",
		Posted:    utc(2026, 5, 6, 10, 15),
		Moderated: true,
	})
	s.comment(store.Comment{
		ID: "c2222222-2222-4222-8222-222222222222", EntryID: e.ID,
		Name: "Scripty", Email: "scripty@example.com", Website: "javascript:alert(1)",
		Comment:   "<script>alert('x')</script> & then some.",
		Posted:    utc(2026, 5, 6, 11, 0),
		Moderated: true,
	})
	s.comment(store.Comment{
		ID: "c3333333-3333-4333-8333-333333333333", EntryID: e.ID,
		Name: "Held", Email: "held@example.com", Comment: "Waiting for approval.",
		Posted: utc(2026, 5, 6, 12, 0), Moderated: false,
	})
	// A subscription is not a comment and never shows.
	s.comment(store.Comment{
		ID: "c4444444-4444-4444-8444-444444444444", EntryID: e.ID,
		Name: "Subscriber", Email: "sub@example.com", Comment: "",
		Posted: utc(2026, 5, 6, 13, 0), Moderated: true, SubscribeOnly: true, Subscribe: true,
	})

	body := s.getOK("/2026/5/6/talkative")
	block := commentsBlock.FindString(body)
	if block == "" {
		t.Fatalf("no comment list on the entry page:\n%s", body)
	}
	permalink := testBase + "/2026/5/6/talkative"
	for _, want := range []string{
		`<a name="comments"></a>`,
		`<h3 class="commentHeader">Comments (2)</h3>`,
		`<li class="comment" id="cc1111111-1111-4111-8111-111111111111">`,
		`<a class="comment-id" href="` + permalink + `#cc1111111-1111-4111-8111-111111111111">#1</a>`,
		`<a href="http://pete.example/blog" rel="nofollow">Pete F</a>`,
		`said on May 6, 2026 at 10:15 AM`,
		`First line.<br />Second line.<br /><br />See `,
		`<a href="http://www.coldfusionjedi.com/index.cfm" rel="nofollow noopener" target="_blank">`,
		`<li class="comment commentAlt" id="cc2222222-2222-4222-8222-222222222222">`,
		`&lt;script&gt;alert(&#39;x&#39;)&lt;/script&gt; &amp; then some.`,
	} {
		if !strings.Contains(block, want) {
			t.Errorf("the comment list is missing %q:\n%s", want, block)
		}
	}
	// An unmoderated comment and a subscription row never appear, and the
	// count over the list counts neither.
	for _, hidden := range []string{"Waiting for approval", "Subscriber"} {
		if strings.Contains(block, hidden) {
			t.Errorf("the comment list shows %q:\n%s", hidden, block)
		}
	}
	// A website that is not http(s) is dropped rather than linked.
	if strings.Contains(block, "javascript:") {
		t.Errorf("a javascript: website was linked:\n%s", block)
	}
	// The count also reaches the header anchor and the footer line.
	if !strings.Contains(body, `class="comments">2 Comments</a>`) {
		t.Error("the entry header does not show the real comment count")
	}
	if !strings.Contains(body, "There are currently 2 comments.") {
		t.Error("the entry footer does not show the real comment count")
	}

	// Gravatars are on in the seeded settings: md5 of the trimmed,
	// lowercased address, size 64, with the blog's own default image.
	gravatar := `https://www.gravatar.com/avatar/` +
		fmt.Sprintf("%x", md5.Sum([]byte("pete@example.com"))) +
		`?s=64&amp;r=pg&amp;d=` + url.QueryEscape(testBase+"/static/images/gravatar.gif")
	if !strings.Contains(block, gravatar) {
		t.Errorf("the comment list is missing the Gravatar %q:\n%s", gravatar, block)
	}
	checkGolden(t, "comments_list.html", block)

	// Turned off, no avatar is emitted at all.
	s.setSetting("allowgravatars", "no")
	if off := commentsBlock.FindString(s.getOK("/2026/5/6/talkative")); strings.Contains(off, "gravatar.com") {
		t.Errorf("gravatars are off but an avatar was rendered:\n%s", off)
	}
}

// TestFP_P15_CommentsNotAllowedMessage is P15: an entry that disallows
// comments shows the bundle's string where the add-comment link goes.
func TestFP_P15_CommentsNotAllowedMessage(t *testing.T) {
	s := newTestSite(t, nil)
	s.user("ray", "Raymond Camden")
	open := s.entry(store.Entry{
		ID: "d1111111-1111-4111-8111-111111111111", Title: "Open", Alias: "open",
		Posted: utc(2026, 5, 7, 9, 0), Username: "ray", Released: true, AllowComments: true,
	})
	closed := s.entry(store.Entry{
		ID: "d2222222-2222-4222-8222-222222222222", Title: "Closed", Alias: "closed",
		Posted: utc(2026, 5, 8, 9, 0), Username: "ray", Released: true, AllowComments: false,
	})

	onOpen := s.getOK("/2026/5/7/open")
	if !strings.Contains(onOpen, `<a href="`+testBase+`/comments/add/`+open.ID+`">Add Comment</a>`) {
		t.Error("an entry that allows comments has no add-comment link")
	}
	if strings.Contains(onOpen, "Comments are not allowed") {
		t.Error("an entry that allows comments says they are not allowed")
	}

	onClosed := s.getOK("/2026/5/8/closed")
	if !strings.Contains(onClosed, `<div class="commentsnotallowed">Comments are not allowed for this entry.</div>`) {
		t.Error("an entry that disallows comments is missing the message")
	}
	if strings.Contains(onClosed, "/comments/add/"+closed.ID) {
		t.Error("an entry that disallows comments still offers the form")
	}
}

// TestFP_P16_EmptyStates is P16: "no entries" on an empty blog, "no
// entries for your criteria" on a listing that has a filter but no rows.
func TestFP_P16_EmptyStates(t *testing.T) {
	const (
		noEntries   = "There are no blog entries available."
		noForCriter = "There are no blog entries available that match your criteria."
	)
	s := newTestSite(t, nil)

	home := s.getOK("/")
	if !strings.Contains(home, "<h3>Sorry</h3>") {
		t.Error("the empty home page has no Sorry heading")
	}
	if !strings.Contains(home, noEntries) {
		t.Errorf("the empty home page does not say %q", noEntries)
	}
	if strings.Contains(home, noForCriter) {
		t.Error("the empty home page blames the visitor's criteria")
	}

	// A category that exists and has nothing in it, and an archive month
	// with nothing in it: both are valid, both are empty.
	s.category("11111111-1111-4111-8111-111111111111", "ColdFusion", "coldfusion")
	for _, path := range []string{"/coldfusion", "/2026/5", "/2026/5/9"} {
		body := s.getOK(path)
		if !strings.Contains(body, noForCriter) {
			t.Errorf("GET %s does not say %q", path, noForCriter)
		}
		if strings.Contains(body, "<p>"+noEntries+"</p>") {
			t.Errorf("GET %s uses the empty-blog string", path)
		}
	}
}

// TestFP_P17_StaticPageWithAndWithoutLayoutUnknownRedirects is P17:
// /page/{alias} inside the layout when the page says so, bare when it
// does not, and home when the alias is nobody's.
func TestFP_P17_StaticPageWithAndWithoutLayoutUnknownRedirects(t *testing.T) {
	s := newTestSite(t, nil)
	if err := s.store.CreateTextblock(context.Background(),
		&store.Textblock{Label: "greeting", Body: "Hello from a textblock."}); err != nil {
		t.Fatalf("create textblock: %v", err)
	}
	s.page(store.Page{
		ID: "e1111111-1111-4111-8111-111111111111", Title: "About Me", Alias: "about",
		Body: "First paragraph.\n\n<textblock label=\"greeting\">", ShowLayout: true,
	})
	s.page(store.Page{
		ID: "e2222222-2222-4222-8222-222222222222", Title: "Bare", Alias: "bare",
		Body: "Nothing around me.", ShowLayout: false,
	})

	withLayout := s.getOK("/page/about")
	for _, want := range []string{
		`<div id="page" class="with-sidebar">`,
		`<title>BlogCFC - About Me</title>`,
		`<div class="date"><b>About Me</b></div>`,
		`<p>First paragraph.</p>`,
		`Hello from a textblock.`,
	} {
		if !strings.Contains(withLayout, want) {
			t.Errorf("the page with a layout is missing %q", want)
		}
	}

	bare := s.getOK("/page/bare")
	if strings.Contains(bare, "<html") || strings.Contains(bare, `id="sidebar"`) {
		t.Errorf("the page without a layout brought one along:\n%s", bare)
	}
	if strings.TrimSpace(bare) != "<p>Nothing around me.</p>" {
		t.Errorf("the bare page = %q, want the rendered body alone", bare)
	}

	for _, path := range []string{"/page/nosuchpage", "/page/"} {
		rec := s.get(path)
		if rec.Code != http.StatusFound {
			t.Errorf("GET %s = %d, want 302", path, rec.Code)
		}
		if got := rec.Header().Get("Location"); got != testBase+"/" {
			t.Errorf("GET %s redirected to %q, want the blog's home", path, got)
		}
	}
}

// TestFP_P18_PrintViewRendersBodyAndCode is P18: the print view prints
// the title, the byline, body and morebody, with code blocks escaped into
// pre.codePrint; it counts no view and 404s on an id nobody has.
func TestFP_P18_PrintViewRendersBodyAndCode(t *testing.T) {
	s := newTestSite(t, nil)
	s.user("ray", "Raymond Camden")
	cat := s.category("11111111-1111-4111-8111-111111111111", "ColdFusion", "coldfusion")
	e := s.entry(store.Entry{
		ID: "f1111111-1111-4111-8111-111111111111", Title: "Printable", Alias: "printable",
		Body:     "Before the code.\n\n<code><cfset x = 1></code>",
		MoreBody: "After the jump.",
		Posted:   utc(2026, 5, 10, 14, 30), Username: "ray", Released: true, Views: 5,
	})
	s.categorise(e.ID, cat.ID)

	rec := s.get("/print/" + e.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /print/%s = %d, want 200", e.ID, rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`<title>BlogCFC: Printable</title>`,
		`<h1>Printable</h1>`,
		`Posted At : May 10, 2026 at 2:30 PM`,
		`Posted By : ray`,
		`Related Categories: ColdFusion`,
		`<p>Before the code.</p>`,
		`<pre class="codePrint">&lt;cfset x = 1&gt;</pre>`,
		`<p>After the jump.</p>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the print view is missing %q:\n%s", want, body)
		}
	}
	// No layout, no sidebar, no navigation: print.cfm had none of it.
	if strings.Contains(body, `id="sidebar"`) || strings.Contains(body, `id="nav"`) {
		t.Errorf("the print view brought the layout along:\n%s", body)
	}
	if got := s.views(e.ID); got != 5 {
		t.Errorf("the print view counted a view: %d, want 5", got)
	}
	checkGolden(t, "print_entry.html", printBlock.FindString(body))

	if rec := s.get("/print/f9999999-9999-4999-8999-999999999999"); rec.Code != http.StatusNotFound {
		t.Errorf("GET /print/{unknown} = %d, want 404", rec.Code)
	}
}
