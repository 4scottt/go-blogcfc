package admin_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/4scottt/go-blogcfc/internal/store"
)

// postForm posts a form to an admin path with the harness's cookie jar.
func (h *harness) postForm(path string, form url.Values) (*http.Response, string) {
	h.t.Helper()
	resp, err := h.client.PostForm(h.server.URL+path, form)
	if err != nil {
		h.t.Fatalf("POST %s: %v", path, err)
	}
	return resp, readBody(h.t, resp)
}

// setSetting writes one setting and reloads the accessor the module holds.
func (h *harness) setSetting(key, value string) {
	h.t.Helper()
	if err := h.settings.Set(context.Background(), map[string]string{key: value}); err != nil {
		h.t.Fatalf("set %s=%s: %v", key, value, err)
	}
}

// createEntry puts an entry in the database and returns it.
func (h *harness) createEntry(e *store.Entry) *store.Entry {
	h.t.Helper()
	if err := h.store.CreateEntry(context.Background(), e); err != nil {
		h.t.Fatalf("CreateEntry(%s): %v", e.Title, err)
	}
	return e
}

// createCategory puts a category in the database and returns it.
func (h *harness) createCategory(name, alias string) *store.Category {
	h.t.Helper()
	c := &store.Category{Name: name, Alias: alias}
	if err := h.store.CreateCategory(context.Background(), c); err != nil {
		h.t.Fatalf("CreateCategory(%s): %v", name, err)
	}
	return c
}

// entryByAlias reads an entry back, with its categories.
func (h *harness) entryByAlias(alias string) *store.Entry {
	h.t.Helper()
	e, err := h.store.GetEntryByAlias(context.Background(), alias)
	if err != nil {
		h.t.Fatalf("GetEntryByAlias(%s): %v", alias, err)
	}
	return e
}

// redirectedTo fails unless the response is a 302 to a location with this
// prefix, and returns the location.
func redirectedTo(t *testing.T, resp *http.Response, prefix string) string {
	t.Helper()
	loc := resp.Header.Get("Location")
	if resp.StatusCode != http.StatusFound || !strings.HasPrefix(loc, prefix) {
		t.Fatalf("status %d to %q, want 302 to %s…", resp.StatusCode, loc, prefix)
	}
	return loc
}

// order returns the positions of each needle in body, failing when one is
// missing: the way a sorted table is checked.
func order(t *testing.T, body string, needles ...string) []int {
	t.Helper()
	pos := make([]int, len(needles))
	for i, n := range needles {
		pos[i] = strings.Index(body, n)
		if pos[i] < 0 {
			t.Fatalf("the page does not contain %q", n)
		}
	}
	return pos
}

// TestFP_A03_EntriesListFilterSortPagingBulkDeleteAndViewLink covers PLAN
// §9 A03: the keyword filter, the sortable columns, the page size from
// `maxentriesadmin`, the bulk delete and the View link with `adminview`
// (admin/entries.cfm).
func TestFP_A03_EntriesListFilterSortPagingBulkDeleteAndViewLink(t *testing.T) {
	h := newHarness(t)
	h.user("admin", "Admin")
	now := time.Now().UTC()

	alpha := h.createEntry(&store.Entry{Title: "Alpha needle entry", Alias: "alpha", Body: "b", Posted: now.Add(-4 * time.Hour), Username: "admin", Released: true, Views: 3})
	bravo := h.createEntry(&store.Entry{Title: "Bravo entry", Alias: "bravo", Body: "b", Posted: now.Add(-3 * time.Hour), Username: "writer", Released: true, Views: 90})
	charlie := h.createEntry(&store.Entry{Title: "Charlie needle entry", Alias: "charlie", Body: "b", Posted: now.Add(-2 * time.Hour), Username: "admin", Released: false, Views: 20})
	h.setSetting("maxentriesadmin", "2")
	h.login("admin")

	// Default: posted, newest first, two to a page.
	resp, body := h.get("/admin/entries")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("entries: status %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "<h1>Entries</h1>") {
		t.Error("the entries list has no Entries heading")
	}
	if strings.Contains(body, alpha.Title) {
		t.Error("the third-newest entry is on page 1 although the page size is 2")
	}
	if p := order(t, body, charlie.Title, bravo.Title); p[0] > p[1] {
		t.Error("the default order is not posted, newest first")
	}
	if !strings.Contains(body, "Page 1 of 2") {
		t.Error("the list does not say which page of how many this is")
	}
	for _, want := range []string{
		`<input type="checkbox" name="mark" value="` + charlie.ID + `">`,
		`id="markall"`,
		`value="Delete Marked"`,
		`href="/admin/entries/new"`,
		`<a href="/admin/entries/` + charlie.ID + `">`,
		`action="/admin/entries/delete"`,
		`href="/?mode=entry&amp;entry=` + charlie.ID + `&amp;adminview=1">View</a>`,
		`name="keywords"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the list is missing %s", want)
		}
	}
	// Released is shown as yes/no, and the author and view count with it.
	if !strings.Contains(body, "<td>writer</td>") {
		t.Error("the list does not show the author")
	}
	if !strings.Contains(body, "<td class=\"number\">90</td>") {
		t.Error("the list does not show the view count")
	}

	// Page 2 has the rest.
	_, body = h.get("/admin/entries?page=2")
	if !strings.Contains(body, alpha.Title) || strings.Contains(body, charlie.Title) {
		t.Error("page 2 does not hold the entries page 1 left out")
	}

	// Sorting: the headers carry the links, and the links order the table.
	_, body = h.get("/admin/entries")
	if !strings.Contains(body, "/admin/entries?dir=asc&amp;sort=title") {
		t.Error("the Title header does not link to a title sort")
	}
	h.setSetting("maxentriesadmin", "20")
	_, body = h.get("/admin/entries?sort=title&dir=asc")
	if p := order(t, body, alpha.Title, bravo.Title, charlie.Title); p[0] > p[1] || p[1] > p[2] {
		t.Error("?sort=title&dir=asc is not ordered by title ascending")
	}
	_, body = h.get("/admin/entries?sort=views&dir=desc")
	if p := order(t, body, bravo.Title, charlie.Title, alpha.Title); p[0] > p[1] || p[1] > p[2] {
		t.Error("?sort=views&dir=desc is not ordered by views descending")
	}

	// The keyword filter.
	_, body = h.get("/admin/entries?keywords=needle")
	if !strings.Contains(body, alpha.Title) || !strings.Contains(body, charlie.Title) {
		t.Error("the keyword filter dropped a matching entry")
	}
	if strings.Contains(body, bravo.Title) {
		t.Error("the keyword filter kept an entry that does not match")
	}
	if !strings.Contains(body, `value="needle"`) {
		t.Error("the filter form does not keep the keywords")
	}

	// Bulk delete.
	resp, _ = h.postForm("/admin/entries/delete", url.Values{"mark": {alpha.ID, bravo.ID}})
	redirectedTo(t, resp, "/admin/entries?deleted=2")
	_, total, err := h.store.ListEntries(context.Background(), store.EntryFilter{})
	if err != nil {
		t.Fatalf("ListEntries: %v", err)
	}
	if total != 1 {
		t.Errorf("after deleting two of three entries, %d remain, want 1", total)
	}
}

// TestFP_A04_NonReleaseUsersSeeOnlyDrafts covers PLAN §9 A04: without the
// ReleaseEntries role the list is drafts only and the released flag cannot
// be set (admin/entries.cfm, admin/entry.cfm).
func TestFP_A04_NonReleaseUsersSeeOnlyDrafts(t *testing.T) {
	h := newHarness(t)
	h.user("writer", "AddCategory")
	now := time.Now().UTC()
	live := h.createEntry(&store.Entry{Title: "A live entry", Alias: "live", Body: "b", Posted: now.Add(-time.Hour), Username: "writer", Released: true})
	draft := h.createEntry(&store.Entry{Title: "A draft entry", Alias: "draft", Body: "b", Posted: now.Add(-time.Hour), Username: "writer"})

	h.login("writer")
	_, body := h.get("/admin/entries")
	if !strings.Contains(body, draft.Title) {
		t.Error("a user without ReleaseEntries cannot see the drafts")
	}
	if strings.Contains(body, live.Title) {
		t.Error("a user without ReleaseEntries sees a released entry")
	}

	// The released control is there but disabled, and carries no name, so
	// nothing is submitted for it.
	_, body = h.get("/admin/entries/new")
	if !strings.Contains(body, `id="released" value="1" disabled`) {
		t.Error("the released checkbox is not disabled for a user who may not release")
	}
	if strings.Contains(body, `name="released"`) {
		t.Error("the released checkbox is submittable for a user who may not release")
	}

	// A hand-made POST with released=1 still makes a draft.
	resp, _ := h.postForm("/admin/entries/new", url.Values{
		"title": {"Sneaky release"}, "body": {"body"}, "released": {"1"}, "save": {"Save"},
	})
	redirectedTo(t, resp, "/admin/entries")
	if got := h.entryByAlias("Sneaky-release"); got.Released {
		t.Error("a user without ReleaseEntries released an entry through the form")
	}

	// Editing a released entry leaves the flag alone rather than clearing
	// it, because the control was never offered.
	resp, _ = h.postForm("/admin/entries/"+live.ID, url.Values{
		"title": {live.Title}, "body": {"b"}, "alias": {live.Alias}, "save": {"Save"},
	})
	redirectedTo(t, resp, "/admin/entries")
	if got := h.entryByAlias(live.Alias); !got.Released {
		t.Error("editing a released entry without the role cleared its released flag")
	}

	// And the bulk delete will not take an entry the list never showed.
	resp, _ = h.postForm("/admin/entries/delete", url.Values{"mark": {live.ID}})
	redirectedTo(t, resp, "/admin/entries?deleted=0")
	if _, err := h.store.GetEntry(context.Background(), live.ID); err != nil {
		t.Errorf("a user without ReleaseEntries deleted a released entry: %v", err)
	}
}

// TestFP_A05_EntryCreateEditMoreSplitCategoriesNewCategoryAliasAndPostedZone
// covers PLAN §9 A05 (admin/entry.cfm): the <more/> split and re-join, the
// leading <more/> refusal, categories and the inline new category, the
// alias made from the title, and `posted` read in the blog's zone.
func TestFP_A05_EntryCreateEditMoreSplitCategoriesNewCategoryAliasAndPostedZone(t *testing.T) {
	h := newHarness(t)
	h.user("admin", "Admin")
	h.user("writer", "ReleaseEntries")
	h.setSetting("timezone", "America/Los_Angeles")
	existing := h.createCategory("Existing", "Existing")

	h.login("admin")
	resp, _ := h.postForm("/admin/entries/new", url.Values{
		"title":         {"Hello World"},
		"body":          {"Intro part<more/>Rest part"},
		"categories":    {existing.ID, "not-a-category"},
		"newcategory":   {"Fresh Category"},
		"posted":        {"2026-03-01 08:00"},
		"alias":         {""},
		"allowcomments": {"1"},
		"sendemail":     {"1"},
		"released":      {"1"},
		"save":          {"Save"},
	})
	redirectedTo(t, resp, "/admin/entries?saved=1")

	// The alias came from the title through render.MakeTitle.
	e := h.entryByAlias("Hello-World")
	if e.Body != "Intro part" || e.MoreBody != "Rest part" {
		t.Errorf("the body was not split on <more/>: body %q, morebody %q", e.Body, e.MoreBody)
	}
	// 08:00 Pacific on 1 March is 16:00 UTC; the store keeps UTC.
	if want := time.Date(2026, 3, 1, 16, 0, 0, 0, time.UTC); !e.Posted.Equal(want) {
		t.Errorf("posted = %s, want %s (the field is read in the blog's zone)", e.Posted, want)
	}
	var names []string
	for _, c := range e.Categories {
		names = append(names, c.Name)
	}
	if len(names) != 2 || !strings.Contains(strings.Join(names, ","), "Existing") || !strings.Contains(strings.Join(names, ","), "Fresh Category") {
		t.Errorf("categories = %v, want Existing and the new Fresh Category (and the unknown id dropped)", names)
	}
	fresh, err := h.store.GetCategoryByAlias(context.Background(), "Fresh-Category")
	if err != nil {
		t.Fatalf("the inline new category was not created with an alias from its name: %v", err)
	}
	if fresh.Name != "Fresh Category" {
		t.Errorf("the new category is named %q", fresh.Name)
	}

	// The editor shows the body re-joined, and the entered time back in
	// the blog's zone.
	_, body := h.get("/admin/entries/" + e.ID)
	if !strings.Contains(body, "Intro part&lt;more/&gt;Rest part") {
		t.Error("the editor does not show the body re-joined around <more/>")
	}
	if !strings.Contains(body, `value="2026-03-01 08:00"`) {
		t.Error("the editor does not show posted in the blog's zone")
	}
	if !strings.Contains(body, `<option value="`+existing.ID+`" selected>Existing</option>`) {
		t.Error("the categories select does not mark the entry's categories")
	}
	for _, want := range []string{`name="title"`, `name="body"`, `name="categories"`, `name="newcategory"`,
		`name="posted"`, `name="alias"`, `name="allowcomments"`, `name="sendemail"`, `name="released"`,
		`name="subtitle"`, `name="keywords"`, `name="summary"`, `name="duration"`,
		`value="Save"`, `id="cancel" href="/admin/entries"`, "Related Entries", "Comments",
		"Available in a later milestone"} {
		if !strings.Contains(body, want) {
			t.Errorf("the entry editor is missing %s", want)
		}
	}
	if strings.Contains(body, `type="file"`) {
		t.Error("the editor offers an enclosure upload, which is a later milestone")
	}

	// A body that starts with <more/> is refused, and nothing is written.
	resp, body = h.postForm("/admin/entries/"+e.ID, url.Values{
		"title": {"Hello World"}, "body": {"<more/>Everything"}, "alias": {e.Alias}, "save": {"Save"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("a leading <more/>: status %d, want the form back with 200", resp.StatusCode)
	}
	if !strings.Contains(body, "may not start with") {
		t.Error("a leading <more/> was refused without a message")
	}
	if again := h.entryByAlias("Hello-World"); again.Body != "Intro part" {
		t.Error("a refused save changed the entry")
	}

	// An alias another entry already uses is refused.
	resp, body = h.postForm("/admin/entries/new", url.Values{
		"title": {"Another"}, "body": {"b"}, "alias": {"Hello-World"}, "save": {"Save"},
	})
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "already uses the alias") {
		t.Errorf("a duplicate alias: status %d, want the form back with a message", resp.StatusCode)
	}

	// A user without AddCategory does not get the field, and a value sent
	// anyway is ignored.
	h.get("/admin/logout")
	h.login("writer")
	_, body = h.get("/admin/entries/new")
	if strings.Contains(body, `name="newcategory"`) {
		t.Error("a user without AddCategory is offered the new-category field")
	}
	resp, _ = h.postForm("/admin/entries/new", url.Values{
		"title": {"Writer entry"}, "body": {"b"}, "newcategory": {"Smuggled"}, "save": {"Save"},
	})
	redirectedTo(t, resp, "/admin/entries?saved=1")
	if _, err := h.store.GetCategoryByAlias(context.Background(), "Smuggled"); err == nil {
		t.Error("a user without AddCategory created a category through newcategory")
	}
	if got := h.entryByAlias("Writer-entry"); len(got.Categories) != 0 {
		t.Errorf("the smuggled category was attached: %v", got.Categories)
	}
}

// TestFP_A06_FlagsAndDraftReleasedWithPastDateGetsPostedNow covers PLAN §9
// A06: the three flags round-trip, and a draft released with a date
// already past is posted now instead (admin/entry.cfm, Shane Zehnder's
// fix).
func TestFP_A06_FlagsAndDraftReleasedWithPastDateGetsPostedNow(t *testing.T) {
	h := newHarness(t)
	h.user("admin", "Admin")
	h.login("admin")

	// A draft, with both flags off and a date well in the past.
	resp, _ := h.postForm("/admin/entries/new", url.Values{
		"title": {"A staged draft"}, "body": {"b"}, "posted": {"2020-01-02 03:04"}, "save": {"Save"},
	})
	redirectedTo(t, resp, "/admin/entries?saved=1")
	e := h.entryByAlias("A-staged-draft")
	if e.Released || e.AllowComments || e.SendEmail {
		t.Errorf("unchecked boxes were stored as set: released %v, allowcomments %v, sendemail %v",
			e.Released, e.AllowComments, e.SendEmail)
	}
	if want := time.Date(2020, 1, 2, 3, 4, 0, 0, time.UTC); !e.Posted.Equal(want) {
		t.Errorf("a draft's posted = %s, want the date as entered (%s)", e.Posted, want)
	}

	// Released now, with that past date still in the field.
	before := time.Now().UTC().Add(-2 * time.Second)
	resp, _ = h.postForm("/admin/entries/"+e.ID, url.Values{
		"title": {"A staged draft"}, "body": {"b"}, "alias": {e.Alias},
		"posted": {"2020-01-02 03:04"}, "released": {"1"},
		"allowcomments": {"1"}, "sendemail": {"1"}, "save": {"Save"},
	})
	redirectedTo(t, resp, "/admin/entries?saved=1")
	e = h.entryByAlias("A-staged-draft")
	if !e.Released || !e.AllowComments || !e.SendEmail {
		t.Errorf("the checked boxes were not stored: released %v, allowcomments %v, sendemail %v",
			e.Released, e.AllowComments, e.SendEmail)
	}
	if e.Posted.Before(before) {
		t.Errorf("a draft released with a past date kept posted = %s, want about now", e.Posted)
	}

	// A released entry backdated on purpose keeps the date: the rule is
	// for the draft-to-released hop only.
	resp, _ = h.postForm("/admin/entries/"+e.ID, url.Values{
		"title": {"A staged draft"}, "body": {"b"}, "alias": {e.Alias},
		"posted": {"2021-05-06 07:08"}, "released": {"1"}, "save": {"Save"},
	})
	redirectedTo(t, resp, "/admin/entries?saved=1")
	if want := time.Date(2021, 5, 6, 7, 8, 0, 0, time.UTC); !h.entryByAlias("A-staged-draft").Posted.Equal(want) {
		t.Errorf("a released entry's backdating was overwritten, want %s", want)
	}

	// A future date on a draft being released is left alone too: that is a
	// scheduled entry (PLAN §11 "Three entry states").
	resp, _ = h.postForm("/admin/entries/new", url.Values{
		"title": {"Scheduled piece"}, "body": {"b"},
		"posted": {time.Now().UTC().Add(48 * time.Hour).Format("2006-01-02 15:04")},
		"save":   {"Save"},
	})
	redirectedTo(t, resp, "/admin/entries?saved=1")
	scheduled := h.entryByAlias("Scheduled-piece")
	resp, _ = h.postForm("/admin/entries/"+scheduled.ID, url.Values{
		"title": {"Scheduled piece"}, "body": {"b"}, "alias": {scheduled.Alias},
		"posted": {scheduled.Posted.Format("2006-01-02 15:04")}, "released": {"1"}, "save": {"Save"},
	})
	redirectedTo(t, resp, "/admin/entries?saved=1")
	if got := h.entryByAlias("Scheduled-piece"); !got.Posted.After(time.Now().UTC()) {
		t.Errorf("a scheduled entry's future date was pulled back to %s", got.Posted)
	}

	// A title is required; an empty one comes back with a message.
	resp, body := h.postForm("/admin/entries/new", url.Values{"title": {"  "}, "body": {"b"}, "save": {"Save"}})
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "must include a title") {
		t.Errorf("an empty title: status %d, want the form back with a message", resp.StatusCode)
	}
}
