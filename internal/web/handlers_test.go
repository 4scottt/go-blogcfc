package web

import (
	"fmt"
	"net/http"
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
	for _, path := range []string{
		"/?mode=cat&catid=66666666-6666-4666-8666-666666666666",
		"/no-such-category",
		"/search", "/rss", "/admin", "/contact", "/sitemap.xml", "/robots.txt",
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
		Body: "<p>The first half.</p>", MoreBody: "<p>The second half.</p>",
		Posted: utc(2026, 3, 5, 14, 30), Username: "ray", Released: true, Views: 42,
	})
	s.categorise(e.ID, cat.ID)
	permalink := testBase + "/2026/3/5/split-entry"

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
	s.entry(store.Entry{Title: "Whole", Alias: "whole", Body: "<p>All of it.</p>",
		Posted: utc(2026, 3, 6, 9, 0), Username: "ray", Released: true})
	if n := strings.Count(s.getOK("/"), ">[More]<"); n != 1 {
		t.Errorf("the home page has %d [more] links, want 1", n)
	}
}
