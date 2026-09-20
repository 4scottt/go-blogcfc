package admin_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// settingsKeys is PLAN §10's list, minus `pods`, which has its own
// screen: every one of them must be on the page by name.
var settingsKeys = []string{
	"blogtitle", "blogdescription", "blogkeywords", "owneremail", "failto", "blogurl",
	"commentsfrom", "maxentries", "maxentriesadmin", "timezone", "pingurls", "locale",
	"ipblocklist", "moderate", "usecaptcha", "usecfp", "usetweetbacks", "trackbackspamlist",
	"allowgravatars", "filebrowse", "imageroot",
	"itunessubtitle", "itunessummary", "ituneskeywords", "itunesauthor", "itunesimage", "itunesexplicit",
}

// goodSettings is a form the page accepts, for a test to vary one field
// of at a time.
func goodSettings() url.Values {
	return url.Values{
		"blogtitle":         {"A Rewritten Blog"},
		"blogdescription":   {"The as-is, in Go."},
		"blogkeywords":      {"go, blogcfc"},
		"owneremail":        {"owner@example.com"},
		"failto":            {"failures@example.com"},
		"commentsfrom":      {"comments@example.com"},
		"maxentries":        {"12"},
		"maxentriesadmin":   {"25"},
		"timezone":          {"America/Los_Angeles"},
		"pingurls":          {"http://zeta.example/ping\r\nhttp://alpha.example/ping"},
		"locale":            {"de_DE"},
		"ipblocklist":       {"10.0.0.1\r\n10.0.0.2"},
		"moderate":          {"no"},
		"usecaptcha":        {"yes"},
		"usecfp":            {"yes"},
		"trackbackspamlist": {"zebra\r\nAlpha\r\n\r\nmiddle"},
		"allowgravatars":    {"no"},
		"filebrowse":        {"yes"},
		"imageroot":         {"/images"},
		"itunessubtitle":    {"A subtitle"},
		"itunessummary":     {"A summary"},
		"ituneskeywords":    {"one, two"},
		"itunesauthor":      {"An Author"},
		"itunesimage":       {"http://example.com/cover.png"},
		"itunesexplicit":    {"no"},
		"save":              {"Save"},
	}
}

// TestFP_A20_SettingsPageFieldsetsValidationsNewlineListsAndSortedSpamList
// covers PLAN §9 A20 and §10: every key on the page, the fieldsets in
// order, the read-only rows, the validations, the newline lists that
// round-trip, the spam list sorted on save, and the cache flush.
func TestFP_A20_SettingsPageFieldsetsValidationsNewlineListsAndSortedSpamList(t *testing.T) {
	h := newHarness(t)
	h.user("admin", "Admin")
	flushes := 0
	h.module.Flush = func() { flushes++ }
	h.login("admin")

	resp, body := h.get("/admin/settings")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("settings: status %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "<h1>Settings</h1>") {
		t.Error("the settings page's heading is not Settings")
	}
	for _, key := range settingsKeys {
		if !strings.Contains(body, `name="`+key+`"`) {
			t.Errorf("the settings form has no control named %q", key)
		}
	}
	if strings.Contains(body, `name="pods"`) {
		t.Error("the pods setting is on the settings page; it has its own screen")
	}

	legends := []string{
		"<legend>Blog Information</legend>", "<legend>Content</legend>",
		"<legend>Content Controls / Security</legend>", "<legend>Data Source and Mail</legend>",
		"<legend>Podcasting</legend>",
	}
	if p := order(t, body, legends...); p[0] > p[1] || p[1] > p[2] || p[2] > p[3] || p[3] > p[4] {
		t.Errorf("the fieldsets are not in the order of PLAN §10: %v", p)
	}

	// The environment's rows are shown and never editable.
	for _, label := range []string{"Database Host", "Database Name", "Database User", "SMTP Host", "SMTP User", "Mail Mode"} {
		if !strings.Contains(body, label) {
			t.Errorf("the Data Source and Mail fieldset has no %q row", label)
		}
	}
	// blogurl mirrors BLOG_BASE_URL and is shown read-only.
	if !strings.Contains(body, `name="blogurl" value="http://127.0.0.1:8080" maxlength="255" readonly`) {
		t.Error("blogurl is not a read-only row carrying BLOG_BASE_URL")
	}
	// usetweetbacks is kept but inert.
	if !strings.Contains(body, `name="usetweetbacks" disabled`) {
		t.Error("usetweetbacks is not rendered disabled")
	}
	if flushes != 0 {
		t.Errorf("the flush hook ran %d times on a plain GET", flushes)
	}

	// A save.
	resp, body = h.postForm("/admin/settings", goodSettings())
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/admin/settings?saved=1" {
		t.Fatalf("save: status %d to %q, want 302 to the page with ?saved=1 (body: %s)",
			resp.StatusCode, resp.Header.Get("Location"), body)
	}
	if flushes != 1 {
		t.Errorf("the flush hook ran %d times on a save, want 1", flushes)
	}

	_, body = h.get("/admin/settings?saved=1")
	if !strings.Contains(body, "Settings saved") {
		t.Error("a save shows no banner")
	}

	// The accessor sees the write, reloaded.
	if got := h.settings.BlogTitle(); got != "A Rewritten Blog" {
		t.Errorf("blogtitle = %q, want the saved one", got)
	}
	if got := h.settings.MaxEntries(); got != 12 {
		t.Errorf("maxentries = %d, want 12", got)
	}
	if got := h.settings.Timezone().String(); got != "America/Los_Angeles" {
		t.Errorf("timezone = %q, want America/Los_Angeles", got)
	}
	if h.settings.Moderate() {
		t.Error("moderate = yes, want no")
	}
	if h.settings.Locale() != "de_DE" {
		t.Errorf("locale = %q, want de_DE", h.settings.Locale())
	}
	// The disabled setting was not written.
	if h.settings.String("usetweetbacks") != "no" {
		t.Errorf("usetweetbacks = %q, want the untouched no", h.settings.String("usetweetbacks"))
	}

	// Newline lists round-trip exactly, in the order they were typed, with
	// no trailing newline; the spam list is the one that gets sorted.
	wantPings := "http://zeta.example/ping\nhttp://alpha.example/ping"
	if got := h.settings.String("pingurls"); got != wantPings {
		t.Errorf("pingurls = %q, want %q", got, wantPings)
	}
	if got := h.settings.String("ipblocklist"); got != "10.0.0.1\n10.0.0.2" {
		t.Errorf("ipblocklist = %q, want the two lines", got)
	}
	if got := h.settings.String("trackbackspamlist"); got != "Alpha\nmiddle\nzebra" {
		t.Errorf("trackbackspamlist = %q, want it sorted and blank-free", got)
	}
	if !strings.Contains(body, wantPings) {
		t.Error("the pingurls textarea does not show the stored list unchanged")
	}

	// Saving the page again, unchanged, leaves the lists as they are.
	again := goodSettings()
	again.Set("pingurls", wantPings)
	again.Set("trackbackspamlist", "Alpha\nmiddle\nzebra")
	if resp, _ := h.postForm("/admin/settings", again); resp.StatusCode != http.StatusFound {
		t.Fatalf("second save: status %d, want 302", resp.StatusCode)
	}
	if got := h.settings.String("pingurls"); got != wantPings {
		t.Errorf("pingurls after a second save = %q, want %q", got, wantPings)
	}

	// Validations. Each one is refused whole: nothing is written, nothing
	// is flushed.
	flushesBefore := flushes
	for _, bad := range []struct{ name, key, value, message string }{
		{"blank title", "blogtitle", "", "must have a title"},
		{"owner email", "owneremail", "not-an-address", "valid email address"},
		{"fail to", "failto", "also@not@valid", "valid email address"},
		{"comments from", "commentsfrom", "nope", "valid email address"},
		{"max entries", "maxentries", "0", "1 or more"},
		{"max entries admin", "maxentriesadmin", "not-a-number", "1 or more"},
		{"time zone", "timezone", "Mars/Phobos", "not a zone this host knows"},
		{"blank time zone", "timezone", "", "cannot be blank"},
		{"locale", "locale", "fr_FR", "not one this blog has strings for"},
	} {
		form := goodSettings()
		form.Set(bad.key, bad.value)
		form.Set("blogdescription", "a description the refused save must show back")
		resp, body := h.postForm("/admin/settings", form)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s: status %d, want 200 with errors", bad.name, resp.StatusCode)
			continue
		}
		if !strings.Contains(body, bad.message) {
			t.Errorf("%s: the page does not say %q", bad.name, bad.message)
		}
		if !strings.Contains(body, "a description the refused save must show back") {
			t.Errorf("%s: the refused save lost what was typed", bad.name)
		}
		if got := h.settings.String(bad.key); got == bad.value && bad.value != "" {
			t.Errorf("%s: the refused value %q was written anyway", bad.name, got)
		}
	}
	if flushes != flushesBefore {
		t.Errorf("the flush hook ran %d times on refused saves", flushes-flushesBefore)
	}
	if h.settings.BlogTitle() != "A Rewritten Blog" {
		t.Errorf("a refused save changed blogtitle to %q", h.settings.BlogTitle())
	}
}
