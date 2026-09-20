package pods

import (
	"html/template"
	"log/slog"
	"net/url"
	"strconv"

	"github.com/4scottt/go-blogcfc/internal/i18n"
	"github.com/4scottt/go-blogcfc/internal/store"
	"github.com/4scottt/go-blogcfc/internal/web"
)

// The listing pods: archives by subject, monthly archives, recent
// entries, recent comments, the search box, the RSS button and the pages
// navigation (PLAN §9 D02-D06, D09).

// linkView is a label, a URL and an optional count.
type linkView struct {
	Label string
	URL   string
	Count int
	// Extra is the archives pod's per-category RSS link.
	Extra string
	Title string
}

// listView is what templates/list.html draws: a list of links, or a
// message when there is nothing to list.
type listView struct {
	Items []linkView
	Empty string
}

// archivesPod is archives.cfm: every category with its live entry count
// and a per-category RSS link (PLAN §9 D02).
func (m *Module) archivesPod(c *podCtx) (string, template.HTML) {
	title := c.bundle.T("archivesbysubject")
	cats, err := m.store.ListCategories(c.ctx)
	if err != nil {
		return title, fail(Archives, err)
	}
	var v listView
	for _, cat := range cats {
		v.Items = append(v.Items, linkView{
			Label: cat.Name,
			URL:   web.CategoryURL(c.base, cat),
			Count: cat.EntryCount,
			Title: cat.Name + " RSS",
			Extra: c.base + "/rss?mode=full&mode2=cat&catid=" + url.QueryEscape(cat.ID),
		})
	}
	return title, m.exec("archives", v)
}

// archiveYears is BlogCFC's getArchives(archiveYears=5) default: the
// monthly archives pod shows the last five years (PLAN §9 D03).
const archiveYears = 5

// monthlyArchivesPod is monthlyarchives.cfm.
func (m *Module) monthlyArchivesPod(c *podCtx) (string, template.HTML) {
	title := c.bundle.T("archivesbymonth")
	months, err := m.store.MonthlyArchives(c.ctx, archiveYears, c.loc)
	if err != nil {
		return title, fail(MonthlyArchives, err)
	}
	var v listView
	for _, mc := range months {
		v.Items = append(v.Items, linkView{
			Label: i18n.MonthName(c.bundle.Locale(), mc.Month) + " " + strconv.Itoa(mc.Year),
			URL:   monthURL(c.base, mc.Year, mc.Month),
			Count: mc.Count,
		})
	}
	return title, m.exec("list", v)
}

// recentEntries is recent.cfm's maxEntries.
const recentEntries = 5

// recentPod is recent.cfm: the five newest live entries (PLAN §9 D04).
func (m *Module) recentPod(c *podCtx) (string, template.HTML) {
	title := c.bundle.T("recententries")
	entries, _, err := m.store.ListEntries(c.ctx, store.EntryFilter{
		LiveOnly: true, Sort: "posted", Desc: true, Limit: recentEntries,
	})
	if err != nil {
		return title, fail(Recent, err)
	}
	v := listView{Empty: c.bundle.T("norecententries")}
	for _, e := range entries {
		v.Items = append(v.Items, linkView{Label: e.Title, URL: web.EntryURL(c.base, e, c.loc)})
	}
	return title, m.exec("list", v)
}

// recentcomments.cfm's two constants: five comments, cut at 100
// characters (PLAN §9 D05).
const (
	recentCommentCount  = 5
	recentCommentLength = 100
)

// commentView is one line of the recent comments pod.
type commentView struct {
	EntryTitle string
	EntryURL   string
	Name       string
	Said       string
	Excerpt    string
	Ellipsis   bool
	MoreLabel  string
	MoreURL    string
}

// recentCommentsPod is recentcomments.cfm.
func (m *Module) recentCommentsPod(c *podCtx) (string, template.HTML) {
	title := c.bundle.T("recentcomments")
	comments, err := m.store.RecentComments(c.ctx, recentCommentCount)
	if err != nil {
		return title, fail(RecentComments, err)
	}
	v := struct {
		Items []commentView
		Empty string
	}{Empty: c.bundle.T("norecentcomments")}
	seen := map[string]string{} // entry id -> permalink
	for _, rc := range comments {
		link, ok := seen[rc.EntryID]
		if !ok {
			// The permalink needs the entry's alias and posted date, which
			// the comment row does not carry; five comments cost at most
			// five lookups and a deleted entry costs its line, not the pod.
			e, err := m.store.GetEntry(c.ctx, rc.EntryID)
			if err != nil {
				slog.Warn("pods: recent comment without an entry", "entry", rc.EntryID, "error", err)
				continue
			}
			link = web.EntryURL(c.base, *e, c.loc)
			seen[rc.EntryID] = link
		}
		excerpt, cut := truncate(rc.Comment.Comment, recentCommentLength)
		v.Items = append(v.Items, commentView{
			EntryTitle: rc.EntryTitle,
			EntryURL:   link,
			Name:       rc.Name,
			Said:       c.bundle.T("said"),
			Excerpt:    excerpt,
			Ellipsis:   cut,
			MoreLabel:  c.bundle.T("more"),
			MoreURL:    link + "#c" + rc.ID,
		})
	}
	return title, m.exec("recentcomments", v)
}

// truncate cuts a comment to n characters, reporting whether it lost any.
// It counts runes, not bytes, so a German comment is not cut mid-letter.
func truncate(s string, n int) (string, bool) {
	r := []rune(s)
	if len(r) <= n {
		return s, false
	}
	return string(r[:n]), true
}

// searchPod is search.cfm: the search box (PLAN §9 D06).
func (m *Module) searchPod(c *podCtx) (string, template.HTML) {
	label := c.bundle.T("search")
	return label, m.exec("search", struct {
		ActionURL string
		Label     string
	}{c.base + "/search", label})
}

// rssPod is rss.cfm: the feed button (PLAN §9 D09).
func (m *Module) rssPod(c *podCtx) (string, template.HTML) {
	return "RSS", m.exec("rss", struct{ URL string }{c.base + "/rss?mode=full"})
}

// pagesPod is pages.cfm: home and every static page (PLAN §9 D09).
func (m *Module) pagesPod(c *podCtx) (string, template.HTML) {
	title := text(c.bundle, "navigation", "NAVIGATION")
	pages, err := m.store.ListPages(c.ctx)
	if err != nil {
		return title, fail(Pages, err)
	}
	v := listView{Items: []linkView{{
		Label: text(c.bundle, "home", "Home"),
		URL:   c.base + "/",
	}}}
	for _, p := range pages {
		v.Items = append(v.Items, linkView{
			Label: p.Title,
			URL:   c.base + "/page/" + url.PathEscape(p.Alias),
		})
	}
	return title, m.exec("list", v)
}
