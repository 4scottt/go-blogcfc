package pods

import (
	"strings"
	"testing"
	"time"

	"github.com/4scottt/go-blogcfc/internal/store"
)

// TestFP_D02_ArchivesBySubject: every category, its live entry count and
// its own feed (PLAN §9 D02, client/includes/pods/archives.cfm).
func TestFP_D02_ArchivesBySubject(t *testing.T) {
	s := newTestSite(t)
	s.only(Archives)

	cf := s.categoryWithEntries("ColdFusion", "coldfusion", 2)
	s.category("Empty", "empty")
	// A draft in a category does not count towards it.
	draft := s.entry(store.Entry{Title: "Draft", Alias: "draft-cat", Released: false,
		Posted: time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)})
	if err := s.store.SetEntryCategories(newContext(), draft.ID, []string{cf.ID}); err != nil {
		t.Fatalf("set categories: %v", err)
	}

	got := s.sidebar("/")
	mustContain(t, got,
		`<h4>Archives by Subject</h4>`,
		`<a href="`+testBase+`/coldfusion" title="ColdFusion RSS">ColdFusion (2)</a>`,
		`[<a href="`+testBase+`/rss?mode=full&amp;mode2=cat&amp;catid=`+cf.ID+`" rel="noindex,nofollow">RSS</a>]`,
		// archives.cfm stopped hiding empty categories in 5.0.
		`<a href="`+testBase+`/empty" title="Empty RSS">Empty (0)</a>`,
	)
}

// TestFP_D03_MonthlyArchives: the months of the last five years with
// their counts (PLAN §9 D03, pods/monthlyarchives.cfm).
func TestFP_D03_MonthlyArchives(t *testing.T) {
	s := newTestSite(t)
	s.only(MonthlyArchives)

	for day := 1; day <= 3; day++ {
		s.entry(store.Entry{Title: "September " + itoa(day), Alias: "sep-" + itoa(day), Released: true,
			Posted: time.Date(2026, time.September, day, 19, 0, 0, 0, time.UTC)})
	}
	s.entry(store.Entry{Title: "August", Alias: "aug", Released: true,
		Posted: time.Date(2026, time.August, 4, 19, 0, 0, 0, time.UTC)})
	// Older than archiveYears=5: out of the list.
	s.entry(store.Entry{Title: "Ancient", Alias: "ancient", Released: true,
		Posted: time.Date(2010, time.May, 4, 19, 0, 0, 0, time.UTC)})

	got := s.sidebar("/")
	mustContain(t, got,
		`<h4>Archives by Month</h4>`,
		`<a href="`+testBase+`/2026/9">September 2026 (3)</a>`,
		`<a href="`+testBase+`/2026/8">August 2026 (1)</a>`,
	)
	mustNotContain(t, got, "May 2010", testBase+"/2010/5")
	if i, j := strings.Index(got, "September 2026"), strings.Index(got, "August 2026"); i > j {
		t.Errorf("the newest month is not first:\n%s", got)
	}
}

// TestFP_D04_RecentEntries: the five newest live entries (PLAN §9 D04,
// pods/recent.cfm).
func TestFP_D04_RecentEntries(t *testing.T) {
	s := newTestSite(t)
	s.only(Recent)

	if got := s.sidebar("/"); !strings.Contains(got, "No recent entries.") {
		t.Errorf("an empty blog does not say so:\n%s", got)
	}

	base := time.Date(2026, time.September, 1, 19, 0, 0, 0, time.UTC)
	for i := 1; i <= 6; i++ {
		s.entry(store.Entry{Title: "Entry " + itoa(i), Alias: "entry-" + itoa(i), Released: true,
			Posted: base.Add(time.Duration(i) * time.Hour)})
	}
	s.entry(store.Entry{Title: "A draft", Alias: "a-draft", Released: false, Posted: base.Add(9 * time.Hour)})
	s.entry(store.Entry{Title: "Scheduled", Alias: "scheduled", Released: true, Posted: time.Now().AddDate(0, 1, 0)})

	got := s.sidebar("/")
	mustContain(t, got, `<h4>Recent Entries</h4>`, `<a href="`+testBase+`/2026/9/1/entry-6">Entry 6</a>`)
	mustNotContain(t, got, "Entry 1<", "A draft", "Scheduled")
	if n := strings.Count(got, "<li><a "); n != recentEntries {
		t.Errorf("the pod lists %d entries, want %d:\n%s", n, recentEntries, got)
	}
}

// TestFP_D05_RecentComments: five comments as "{entry}: {name} said:
// {100 characters}... [More]", the More anchor on the comment itself
// (PLAN §9 D05, pods/recentcomments.cfm).
func TestFP_D05_RecentComments(t *testing.T) {
	s := newTestSite(t)
	s.only(RecentComments)

	if got := s.sidebar("/"); !strings.Contains(got, "No recent Comments") {
		t.Errorf("an empty blog does not say so:\n%s", got)
	}

	e := s.entry(store.Entry{Title: "Commented entry", Alias: "commented-entry", Released: true,
		Posted: time.Date(2026, time.September, 2, 2, 0, 0, 0, time.UTC)})
	long := strings.Repeat("a", 120)
	c1 := s.comment(store.Comment{EntryID: e.ID, Name: "Ray", Email: "ray@example.com",
		Comment: long, Posted: time.Date(2026, time.September, 3, 1, 0, 0, 0, time.UTC)})
	c2 := s.comment(store.Comment{EntryID: e.ID, Name: "Scott", Email: "scott@example.com",
		Comment: "Short one.", Posted: time.Date(2026, time.September, 4, 1, 0, 0, 0, time.UTC)})
	// A draft's comments never reach the sidebar.
	d := s.entry(store.Entry{Title: "Draft", Alias: "draft-comments", Released: false,
		Posted: time.Date(2026, time.September, 2, 2, 0, 0, 0, time.UTC)})
	s.comment(store.Comment{EntryID: d.ID, Name: "Hidden", Comment: "Hidden comment.",
		Posted: time.Date(2026, time.September, 5, 1, 0, 0, 0, time.UTC)})

	permalink := testBase + "/2026/9/1/commented-entry"
	got := s.sidebar("/")
	mustContain(t, got,
		`<h4>Recent Comments</h4>`,
		`<a href="`+permalink+`">Commented entry</a><br />`,
		`Scott said: Short one. <a href="`+permalink+`#c`+c2.ID+`">[More]</a>`,
		`Ray said: `+strings.Repeat("a", recentCommentLength)+`... <a href="`+permalink+`#c`+c1.ID+`">[More]</a>`,
	)
	mustNotContain(t, got, "Hidden comment", strings.Repeat("a", recentCommentLength+1))
	// Newest first.
	if i, j := strings.Index(got, "Scott said"), strings.Index(got, "Ray said"); i > j {
		t.Errorf("the newest comment is not first:\n%s", got)
	}
}

// TestFP_D09_RSSAndPagesPods: the feed button and the page navigation
// (PLAN §9 D09, pods/rss.cfm and pods/pages.cfm).
func TestFP_D09_RSSAndPagesPods(t *testing.T) {
	s := newTestSite(t)

	s.only(RSS)
	rss := s.sidebar("/")
	checkGolden(t, "rss.html", rss)
	mustContain(t, rss, `<h4>RSS</h4>`, `<a href="`+testBase+`/rss?mode=full" rel="noindex,nofollow">RSS</a>`)

	s.only(Pages)
	s.page(store.Page{Title: "About", Alias: "about", Body: "<p>About.</p>", ShowLayout: true})
	s.page(store.Page{Title: "Contact me", Alias: "contact-me", Body: "<p>Hi.</p>", ShowLayout: true})
	pages := s.sidebar("/")
	checkGolden(t, "pages.html", pages)
	mustContain(t, pages,
		`<h4>NAVIGATION</h4>`,
		`<a href="`+testBase+`/">Home</a>`,
		`<a href="`+testBase+`/page/about">About</a>`,
		`<a href="`+testBase+`/page/contact-me">Contact me</a>`,
	)
}
