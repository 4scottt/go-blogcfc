package web

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/4scottt/go-blogcfc/internal/store"
)

// excerptBlock picks one result's excerpt off the page.
var excerptBlock = regexp.MustCompile(`(?s)<p class="search-excerpt">(.*?)</p>`)

// highlightSpan is the markup search.cfm wrapped a match in.
var highlightSpan = regexp.MustCompile(`<span class="highlight">(.*?)</span>`)

// searchFixtures seeds three live entries that match "needle" and one
// draft that does, plus the long entry whose excerpt is a window rather
// than the whole body.
func searchFixtures(t *testing.T, s *extraSite) (long store.Entry, goCat store.Category) {
	t.Helper()
	s.author("ray", "Raymond Camden")
	goCat = s.category("11111111-1111-4111-8111-111111111111", "Go", "go")
	cf := s.category("22222222-2222-4222-8222-222222222222", "ColdFusion", "coldfusion")
	now := time.Now().UTC().Truncate(time.Second)

	// 600 characters of run-up, the term, then 800 more: the match is
	// well past the 250-character threshold and the body is long enough
	// to be cut on both sides.
	body := "<p>" + strings.Repeat("a ", 300) + "needle" + strings.Repeat(" b", 400) + "</p>"
	long = s.entry(store.Entry{Title: "A long haystack", Alias: "long-haystack", Body: body,
		Posted: now.Add(-time.Hour), Username: "ray", Released: true})
	short := s.entry(store.Entry{Title: "Needle in the title", Alias: "needle-title",
		Body: "<p>Nothing to find in this body.</p>", Posted: now.Add(-2 * time.Hour), Username: "ray", Released: true})
	third := s.entry(store.Entry{Title: "Third", Alias: "third", Body: "<p>One needle here.</p>",
		Posted: now.Add(-3 * time.Hour), Username: "ray", Released: true})
	s.entry(store.Entry{Title: "Draft needle", Alias: "draft-needle", Body: "<p>A needle nobody may see.</p>",
		Posted: now.Add(-4 * time.Hour), Username: "ray"})

	s.categorise(long.ID, goCat.ID)
	s.categorise(short.ID, cf.ID)
	s.categorise(third.ID, cf.ID)
	return long, goCat
}

// TestFP_P19_SearchExcerptsHighlightsCategoryPagingShortcut is P19: the
// term across title, body and morebody, the excerpt window search.cfm
// cuts, the highlight markup, the category filter, the pager and the
// /search/{term} shortcut (PLAN §9 P19).
func TestFP_P19_SearchExcerptsHighlightsCategoryPagingShortcut(t *testing.T) {
	s := newExtraSite(t)
	_, goCat := searchFixtures(t, s)

	// The empty form: the search box, the category select and a submit.
	empty := s.getOK("/search")
	mustContain(t, "the empty search page", empty,
		`<form action="`+testBase+`/search" method="post"`,
		`<input type="text" id="searchterm" name="search" value=""`,
		`<select name="category" id="searchcategory">`,
		`<option value="" selected="selected">all categories</option>`,
		`<option value="`+goCat.ID+`">Go</option>`,
		`value="Search"`,
	)
	if strings.Contains(empty, "search-count") {
		t.Error("an empty search page should not claim a result count")
	}

	body := s.getOK("/search?search=needle")
	mustContain(t, "the search results", body,
		"There were 3 results.",
		`value="Search Again"`,
		`<span class="highlight">needle</span>`,
		`<span class="highlight">Needle</span>`, // the title match keeps its case
		testBase+"/2",                           // the permalinks are absolute
	)
	if strings.Contains(body, "Draft needle") {
		t.Error("a draft answered a public search")
	}

	// The excerpt is the window search.cfm cuts: an ellipsis, 250
	// characters of run-up, the term and what the as-is arithmetic leaves
	// after it (len(term) + 500 characters from the match), an ellipsis.
	excerpts := excerptBlock.FindAllStringSubmatch(body, -1)
	if len(excerpts) != 3 {
		t.Fatalf("got %d excerpts, want 3", len(excerpts))
	}
	window := excerpts[0][1]
	if !strings.HasPrefix(window, "...") || !strings.HasSuffix(window, "...") {
		t.Errorf("the long entry's excerpt is not cut on both sides: %q", window)
	}
	plain := highlightSpan.ReplaceAllString(window, "$1")
	if n := len([]rune(plain)); n != 3+506+3 {
		t.Errorf("the excerpt is %d characters, want %d (... + 250 before + term + 250 after + ...)", n, 3+506+3)
	}
	// The short entry matched on its title alone: its body is shown whole
	// and carries no highlight.
	if got := excerpts[1][1]; got != "Nothing to find in this body." {
		t.Errorf("a body under 500 characters should be shown whole, got %q", got)
	}

	// The shortcut takes the term off the path.
	shortcut := s.getOK("/search/needle")
	mustContain(t, "the /search/{term} shortcut", shortcut,
		"There were 3 results.",
		`<input type="text" id="searchterm" name="search" value="needle"`,
	)

	// The category narrows the same search to one entry.
	byCat := s.getOK("/search?search=needle&category=" + goCat.ID)
	mustContain(t, "the category search", byCat,
		"There was one result.",
		"A long haystack",
		`<option value="`+goCat.ID+`" selected="selected">Go</option>`,
	)
	if strings.Contains(byCat, "Needle in the title") {
		t.Error("the category filter did not narrow the results")
	}

	// Two to a page: the first page offers Next, the second Previous.
	s.setSetting("maxentries", "2")
	first := s.getOK("/search?search=needle")
	mustContain(t, "the first page", first,
		`<a href="`+testBase+`/search?search=needle&amp;start=3">Next Results</a>`,
		"Previous Entries",
	)
	second := s.getOK("/search?search=needle&start=3")
	mustContain(t, "the second page", second,
		`<a href="`+testBase+`/search?search=needle">Previous Results</a>`,
		"Next Entries",
	)
	if n := len(excerptBlock.FindAllString(second, -1)); n != 1 {
		t.Errorf("the second page shows %d results, want the last one", n)
	}

	// A cursor that is not a row number sends the visitor home, as
	// search.cfm's cflocation did.
	rec := s.get("/search?search=needle&start=zero")
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != testBase+"/" {
		t.Errorf("a bad start = %d %q, want a redirect home", rec.Code, rec.Header().Get("Location"))
	}
}

// TestFP_P20_SearchLoggedUnlessPaging is P20: a search is counted once,
// when it is made. Paging through its results and an empty box are not
// searches (PLAN §9 P20, blog.cfc's logSearch).
func TestFP_P20_SearchLoggedUnlessPaging(t *testing.T) {
	s := newExtraSite(t)
	_, goCat := searchFixtures(t, s)
	ctx := context.Background()

	terms := func() []store.TermCount {
		t.Helper()
		got, err := s.store.TopSearchTerms(ctx, 10)
		if err != nil {
			t.Fatalf("TopSearchTerms: %v", err)
		}
		return got
	}

	s.getOK("/search")
	if got := terms(); len(got) != 0 {
		t.Fatalf("the empty form logged %+v", got)
	}

	s.getOK("/search?search=needle")
	got := terms()
	if len(got) != 1 || got[0].Term != "needle" || got[0].Count != 1 {
		t.Fatalf("after one search the stats are %+v, want one `needle`", got)
	}

	// The shortcut is a search too.
	s.getOK("/search/needle")
	if got := terms(); len(got) != 1 || got[0].Count != 2 {
		t.Fatalf("the shortcut logged %+v, want a second `needle`", got)
	}

	// Paging is not.
	s.setSetting("maxentries", "2")
	s.getOK("/search?search=needle&start=3")
	if got := terms(); len(got) != 1 || got[0].Count != 2 {
		t.Fatalf("paging logged %+v, want the two searches only", got)
	}

	// A category-only search has no term to log.
	s.getOK("/search?search=&category=" + goCat.ID)
	if got := terms(); len(got) != 1 || got[0].Count != 2 {
		t.Fatalf("a blank term logged %+v", got)
	}
}
