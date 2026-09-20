package web

import (
	"html"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/4scottt/go-blogcfc/internal/i18n"
	"github.com/4scottt/go-blogcfc/internal/store"
)

// The excerpt window of search.cfm: the first match is shown with 250
// characters of run-up and 500 characters after the term, and an entry
// whose stripped body is no longer than 500 characters is shown whole
// (PLAN §9 P19).
const (
	excerptBefore = 250
	excerptAfter  = 500
	// searchTermMax is search.cfm's left(...,255), which is also the
	// search_stats column's width.
	searchTermMax = 255
)

// tagPattern is search.cfm's `rereplace(body, "<.*?>", "", "all")`: the
// excerpt is plain text, so the entry's markup goes first.
var tagPattern = regexp.MustCompile(`(?s)<.*?>`)

// searchResultView is one hit: the title and the excerpt carry the
// highlight markup this package builds, so both are trusted HTML that
// nothing else may write into.
type searchResultView struct {
	Title   template.HTML
	URL     string
	Posted  string
	Excerpt template.HTML
}

// categoryOption is one row of the category select.
type categoryOption struct {
	ID       string
	Name     string
	Selected bool
}

// searchPage is /search.
type searchPage struct {
	pageData

	Heading     string
	Prompt      string
	ActionURL   string
	SubmitLabel string

	Term       string
	CategoryID string
	Categories []categoryOption

	Searched    bool
	ResultCount string
	Results     []searchResultView

	ShowPager bool
	PrevURL   string
	NextURL   string
}

// handleSearch is `GET|POST /search` and the `GET /search/{term}`
// shortcut (PLAN §8, §9 P19, P20). BlogCFC's search.cfm read the term
// from the URL, the form or the last path segment, in that order, and
// paged with `start`, a 1-based row number like the home page's startRow.
func (m *Module) handleSearch(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// search.cfm's `<cfif not isNumeric(url.start) ...><cflocation
	// url="index.cfm">`: an unusable cursor sends the visitor home.
	start := 1
	if raw := strings.TrimSpace(r.Form.Get("start")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			http.Redirect(w, r, m.base()+"/", http.StatusFound)
			return
		}
		start = n
	}

	// An explicit `search` field wins over the /search/{term} shortcut,
	// as cfparam's default does.
	term := r.PathValue("term")
	if r.Form.Has("search") {
		term = r.Form.Get("search")
	}
	term = truncateRunes(strings.TrimSpace(term), searchTermMax)
	category := strings.TrimSpace(r.Form.Get("category"))

	cats, err := m.store.ListCategories(r.Context())
	if err != nil {
		m.serverError(w, r, err)
		return
	}

	max := m.settings.MaxEntries()
	if max < 1 {
		max = 10
	}

	page := searchPage{
		pageData:    m.newPage(r, m.bundle.T("search")),
		Heading:     m.bundle.T("search"),
		Prompt:      "Search for",
		ActionURL:   m.base() + "/search",
		SubmitLabel: "Search",
		Term:        term,
		CategoryID:  category,
	}
	for _, c := range cats {
		page.Categories = append(page.Categories, categoryOption{ID: c.ID, Name: c.Name, Selected: c.ID == category})
	}

	// Nothing to search for: the empty form, as search.cfm shows it.
	if term == "" && category == "" {
		m.renderExtra(w, "search.html", http.StatusOK, page)
		return
	}

	// P20: the search is logged once, when it is made, not again on every
	// page of its results. BlogCFC logged inside getEntries and so counted
	// a visitor paging through ten results as ten searches.
	if term != "" && start == 1 {
		if err := m.store.LogSearch(r.Context(), term); err != nil {
			slog.Error("web: log search", "term", term, "error", err)
		}
	}

	f := store.EntryFilter{
		LiveOnly: true,
		Keywords: term,
		Sort:     "posted",
		Desc:     true,
		Offset:   start - 1,
		Limit:    max,
	}
	if category != "" {
		f.CategoryIDs = []string{category}
	}
	entries, total, err := m.store.ListEntries(r.Context(), f)
	if err != nil {
		m.serverError(w, r, err)
		return
	}

	page.Searched = true
	page.Prompt = "You searched for"
	page.SubmitLabel = "Search Again"
	if total == 1 {
		page.ResultCount = "There was one result."
	} else {
		page.ResultCount = "There were " + strconv.Itoa(total) + " results."
	}

	base := m.base()
	loc := m.loc()
	locale := m.settings.Locale()
	for _, e := range entries {
		posted := e.Posted.In(loc)
		page.Results = append(page.Results, searchResultView{
			Title:   highlightTerm(e.Title, term),
			URL:     EntryURL(base, e, loc),
			Posted:  i18n.FormatDate(locale, posted, i18n.StyleLong) + " " + i18n.FormatDate(locale, posted, i18n.StyleTime),
			Excerpt: highlightTerm(searchExcerpt(e.Body, term), term),
		})
	}

	// The pager shows whenever there is a page on either side. search.cfm
	// drew it only while more results followed, which lost the Previous
	// link on the last page.
	if start > 1 {
		page.ShowPager = true
		from := start - max
		if from < 1 {
			from = 1
		}
		page.PrevURL = m.searchURL(term, category, from)
	}
	if total > start+max-1 {
		page.ShowPager = true
		page.NextURL = m.searchURL(term, category, start+max)
	}

	m.renderExtra(w, "search.html", http.StatusOK, page)
}

// searchURL is one page of results, with the query kept.
func (m *Module) searchURL(term, category string, start int) string {
	q := url.Values{}
	q.Set("search", term)
	if category != "" {
		q.Set("category", category)
	}
	if start > 1 {
		q.Set("start", strconv.Itoa(start))
	}
	return m.base() + "/search?" + q.Encode()
}

// searchExcerpt is search.cfm's excerpt, character for character: the
// entry's markup is stripped, the first case-insensitive match is found,
// and the window runs from 250 characters before it to 500 after the
// term. A match in the first 250 characters (or no match at all, when
// only the title matched) starts the window at the beginning. Ellipses
// mark a cut on either side. Bodies of 500 characters or fewer are shown
// whole.
func searchExcerpt(body, term string) string {
	// The entities a reader never sees as entities are resolved here, so
	// the excerpt is the text of the entry; the template escapes it again
	// on the way out.
	text := []rune(html.UnescapeString(tagPattern.ReplaceAllString(body, "")))
	if len(text) <= excerptAfter {
		return string(text)
	}

	// CFML's findNoCase is 1-based and answers 0 for no match.
	match := indexFold(text, []rune(term)) + 1
	if match <= excerptBefore {
		match = 1
	}
	end := match + len([]rune(term)) + excerptAfter

	var excerpt string
	if match > 1 {
		// CFML: mid(newbody, match-250, end-match). The count is taken
		// from the match, not from the window's start, so the tail after
		// the term is 250 characters and not 500. That is the as-is
		// arithmetic and it stays.
		from := match - excerptBefore - 1
		excerpt = "..." + string(sliceRunes(text, from, from+end-match))
	} else {
		excerpt = string(sliceRunes(text, 0, end))
	}
	if len(text) > end {
		excerpt += "..."
	}
	return excerpt
}

// highlightTerm escapes the text and wraps every case-insensitive
// occurrence of the term in search.cfm's highlight span. BlogCFC built
// the span with a regular expression over the raw term and fell back to
// no highlighting when the term was not a valid pattern; here the term is
// always a literal, so `c++` highlights as readily as `cat`.
func highlightTerm(text, term string) template.HTML {
	runes := []rune(text)
	needle := []rune(term)
	if len(needle) == 0 {
		return template.HTML(template.HTMLEscapeString(text)) //nolint:gosec // escaped right here
	}
	var b strings.Builder
	for len(runes) > 0 {
		i := indexFold(runes, needle)
		if i < 0 {
			b.WriteString(template.HTMLEscapeString(string(runes)))
			break
		}
		b.WriteString(template.HTMLEscapeString(string(runes[:i])))
		b.WriteString(`<span class="highlight">`)
		b.WriteString(template.HTMLEscapeString(string(runes[i : i+len(needle)])))
		b.WriteString(`</span>`)
		runes = runes[i+len(needle):]
	}
	return template.HTML(b.String()) //nolint:gosec // every run of text above is escaped
}

// indexFold is strings.Index over runes, case-insensitively, without the
// length changes a ToLower of the whole string can bring. It answers the
// rune offset, or -1.
func indexFold(haystack, needle []rune) int {
	if len(needle) == 0 {
		return 0
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		found := true
		for j, r := range needle {
			if unicode.ToLower(haystack[i+j]) != unicode.ToLower(r) {
				found = false
				break
			}
		}
		if found {
			return i
		}
	}
	return -1
}

// sliceRunes is a bounds-safe [from, to) slice, which is what CFML's mid
// and left are: they clamp rather than throw.
func sliceRunes(runes []rune, from, to int) []rune {
	if from < 0 {
		from = 0
	}
	if to > len(runes) {
		to = len(runes)
	}
	if from >= to {
		return nil
	}
	return runes[from:to]
}

// truncateRunes cuts a string to n characters, as CFML's left does.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
