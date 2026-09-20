package admin

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/4scottt/go-blogcfc/internal/i18n"
)

// The kinds a settings field is rendered and read as.
const (
	fieldText     = "text"
	fieldTextarea = "textarea"
	fieldYesNo    = "yesno"
	fieldSelect   = "select"
)

// settingSpec is one row of the settings form: how it is drawn, how it is
// read back, and what it may hold. The form is a table rather than a
// template full of markup so that the keys of PLAN §10 and the page are
// one list, in one order.
type settingSpec struct {
	Key     string
	Label   string
	Kind    string
	Options []string
	Note    string

	// ReadOnly is shown but never written: `blogurl` mirrors
	// BLOG_BASE_URL (PLAN §10) and the accessor drops it anyway.
	ReadOnly bool
	// Disabled is a setting the rewrite keeps inert: it is rendered, it
	// does not post, and a save leaves the stored value alone
	// (`usetweetbacks`, PLAN §7 "Dropped").
	Disabled bool

	// List normalises a newline list on save: CRLF to LF, blank lines
	// dropped, no trailing newline. Sorted also sorts it, case
	// insensitively, as the as-is did to the spam list with
	// listSort(..., "textnocase").
	List   bool
	Sorted bool
}

// settingsFieldsetSpec is one `<fieldset>`; Env is the read-only block
// the environment fills.
type settingsFieldsetSpec struct {
	Legend string
	Fields []settingSpec
	Env    bool
}

// settingsForm is PLAN §10: every key, in its fieldset, in order, with
// the as-is page's labels (admin/settings.cfm) minus their trailing
// colons, which the form rules of PLAN §8 do not allow.
var settingsForm = []settingsFieldsetSpec{
	{Legend: "Blog Information", Fields: []settingSpec{
		{Key: "blogtitle", Label: "Blog Title", Kind: fieldText},
		{Key: "blogdescription", Label: "Blog Description", Kind: fieldTextarea},
		{Key: "blogkeywords", Label: "Blog Keywords", Kind: fieldText},
		{Key: "owneremail", Label: "Owner Email", Kind: fieldText},
		{Key: "failto", Label: "Fail To", Kind: fieldText},
		{Key: "blogurl", Label: "Blog URL", Kind: fieldText, ReadOnly: true,
			Note: "Read-only: every link, feed and mail is built from BLOG_BASE_URL."},
	}},
	{Legend: "Content", Fields: []settingSpec{
		{Key: "commentsfrom", Label: "Comments Sent From", Kind: fieldText},
		{Key: "maxentries", Label: "Max Entries", Kind: fieldText},
		{Key: "maxentriesadmin", Label: "Max Entries Admin", Kind: fieldText},
		{Key: "timezone", Label: "Time Zone", Kind: fieldText,
			Note: "An IANA zone such as America/Los_Angeles. It replaces the as-is numeric offset: entries are stored in UTC and shown in this zone."},
		{Key: "pingurls", Label: "Ping URLs", Kind: fieldTextarea, List: true,
			Note: "One URL per line. Each is fetched once when an entry is released."},
		{Key: "locale", Label: "Locale", Kind: fieldSelect, Options: i18n.Locales},
	}},
	{Legend: "Content Controls / Security", Fields: []settingSpec{
		{Key: "ipblocklist", Label: "IP Block List", Kind: fieldTextarea, List: true,
			Note: "One address per line."},
		{Key: "moderate", Label: "Moderate Comments", Kind: fieldYesNo},
		{Key: "usecaptcha", Label: "Use Captcha", Kind: fieldYesNo,
			Note: "The arithmetic challenge, skipped for a signed-in author."},
		{Key: "usecfp", Label: "Use cfFormProtect", Kind: fieldYesNo,
			Note: "The honeypot, timing and URL-count checks that replace cfFormProtect."},
		{Key: "usetweetbacks", Label: "Use Tweetbacks", Kind: fieldYesNo, Disabled: true,
			Note: "Kept so this page matches the as-is. Tweetbacks are gone, so the setting is inert and a save leaves it alone."},
		{Key: "trackbackspamlist", Label: "Spamlist", Kind: fieldTextarea, List: true, Sorted: true,
			Note: "One term per line. The list is sorted when it is saved."},
		{Key: "allowgravatars", Label: "Allow Gravatars", Kind: fieldYesNo},
		{Key: "filebrowse", Label: "Show File Manager", Kind: fieldYesNo},
		{Key: "imageroot", Label: "Image Root for Dynamic Images", Kind: fieldText},
	}},
	{Legend: "Data Source and Mail", Env: true},
	{Legend: "Podcasting", Fields: []settingSpec{
		{Key: "itunessubtitle", Label: "iTunes Subtitle", Kind: fieldText},
		{Key: "itunessummary", Label: "iTunes Summary", Kind: fieldText},
		{Key: "ituneskeywords", Label: "iTunes Keywords", Kind: fieldText},
		{Key: "itunesauthor", Label: "iTunes Author", Kind: fieldText},
		{Key: "itunesimage", Label: "iTunes Image", Kind: fieldText},
		{Key: "itunesexplicit", Label: "iTunes Explicit", Kind: fieldYesNo},
	}},
}

// settingOption is one `<option>`.
type settingOption struct {
	Value    string
	Label    string
	Selected bool
}

// settingsField is one rendered row.
type settingsField struct {
	Key      string
	Label    string
	Kind     string
	Value    string
	Note     string
	ReadOnly bool
	Disabled bool
	Options  []settingOption
}

// envRow is a read-only line of the Data Source and Mail fieldset: the
// environment, never editable in the page (PLAN §10).
type envRow struct {
	Label string
	Value string
}

// settingsFieldset is one rendered `<fieldset>`.
type settingsFieldset struct {
	Legend  string
	Fields  []settingsField
	EnvRows []envRow
}

// settingsPage is settings.html's own data.
type settingsPage struct {
	Errors    []string
	Fieldsets []settingsFieldset
}

// settingsPageHandler is GET /admin/settings (PLAN §9 A20,
// admin/settings.cfm).
func (m *Module) settingsPageHandler(w http.ResponseWriter, r *http.Request) {
	m.renderSettings(w, r, settingsPage{Fieldsets: m.settingsFieldsets(m.settings.All())}, settingsFlash(r))
}

// settingsSave is POST /admin/settings. Everything is validated before
// anything is written, so a bad zone or a bad address cannot leave half
// the page saved; a refused save re-renders what was typed.
func (m *Module) settingsSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	values := map[string]string{}
	for _, fs := range settingsForm {
		for _, f := range fs.Fields {
			if f.ReadOnly || f.Disabled {
				continue
			}
			values[f.Key] = normalizeSetting(f, r.PostFormValue(f.Key))
		}
	}

	if errs := validateSettings(values); len(errs) > 0 {
		// The current values are the frame; what was typed wins over
		// them, so the operator sees their own mistake.
		shown := m.settings.All()
		for k, v := range values {
			shown[k] = v
		}
		m.renderSettings(w, r, settingsPage{Errors: errs, Fieldsets: m.settingsFieldsets(shown)}, "")
		return
	}

	if err := m.settings.Set(r.Context(), values); err != nil {
		m.serverError(w, r, err)
		return
	}
	// Settings reach the home page, the feeds and the pods, so every
	// cache goes with the write (PLAN §11 "Caching", §9 A29).
	m.flush()
	http.Redirect(w, r, "/admin/settings?saved=1", http.StatusFound)
}

// settingsFlash is the banner a redirect asks for.
func settingsFlash(r *http.Request) string {
	if r.URL.Query().Has("saved") {
		return "Settings saved."
	}
	return ""
}

func (m *Module) renderSettings(w http.ResponseWriter, r *http.Request, p settingsPage, flash string) {
	data := m.newPageData(r, "Settings")
	data.Flash = flash
	data.Page = p
	render(w, "settings.html", data)
}

// settingsFieldsets turns the spec and the current values into what the
// template draws.
func (m *Module) settingsFieldsets(values map[string]string) []settingsFieldset {
	out := make([]settingsFieldset, 0, len(settingsForm))
	for _, fs := range settingsForm {
		rendered := settingsFieldset{Legend: fs.Legend}
		if fs.Env {
			rendered.EnvRows = m.envRows()
			out = append(out, rendered)
			continue
		}
		for _, f := range fs.Fields {
			rendered.Fields = append(rendered.Fields, settingsField{
				Key:      f.Key,
				Label:    f.Label,
				Kind:     f.Kind,
				Value:    values[f.Key],
				Note:     f.Note,
				ReadOnly: f.ReadOnly,
				Disabled: f.Disabled,
				Options:  fieldOptions(f, values[f.Key]),
			})
		}
		out = append(out, rendered)
	}
	return out
}

// envRows is the environment the page shows and never writes.
func (m *Module) envRows() []envRow {
	rows := []envRow{
		{Label: "Database Host", Value: m.cfg.DBHost},
		{Label: "Database Name", Value: m.cfg.DBName},
		{Label: "Database User", Value: m.cfg.DBUser},
		{Label: "SMTP Host", Value: m.cfg.SMTPHost},
		{Label: "SMTP User", Value: m.cfg.SMTPUser},
		{Label: "Mail Mode", Value: m.cfg.MailMode},
	}
	for i, row := range rows {
		if strings.TrimSpace(row.Value) == "" {
			rows[i].Value = "(not set)"
		}
	}
	return rows
}

// fieldOptions builds the options of a select, with the current value
// picked. A yes/no field whose stored value is anything else reads as no,
// exactly as the accessor's Bool does.
func fieldOptions(f settingSpec, value string) []settingOption {
	switch f.Kind {
	case fieldYesNo:
		yes := isYes(value)
		return []settingOption{
			{Value: "yes", Label: "Yes", Selected: yes},
			{Value: "no", Label: "No", Selected: !yes},
		}
	case fieldSelect:
		out := make([]settingOption, 0, len(f.Options))
		for _, o := range f.Options {
			out = append(out, settingOption{Value: o, Label: o, Selected: o == value})
		}
		return out
	default:
		return nil
	}
}

// normalizeSetting is how a submitted value is stored: a newline list
// loses its CRLFs, its blank lines and its trailing newline so that it
// round-trips through the textarea unchanged, and the spam list is
// sorted with it.
func normalizeSetting(f settingSpec, raw string) string {
	switch {
	case f.List:
		lines := []string{}
		for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
			if t := strings.TrimSpace(line); t != "" {
				lines = append(lines, t)
			}
		}
		if f.Sorted {
			sort.SliceStable(lines, func(i, j int) bool {
				return strings.ToLower(lines[i]) < strings.ToLower(lines[j])
			})
		}
		return strings.Join(lines, "\n")
	case f.Kind == fieldYesNo:
		if isYes(raw) {
			return "yes"
		}
		return "no"
	default:
		return strings.TrimSpace(raw)
	}
}

// validateSettings is the as-is page's checks (admin/settings.cfm) with
// the keys PLAN §7 changed: a zone instead of an offset, a known locale
// instead of any string, and numerics that have to be usable page sizes
// rather than merely numeric.
func validateSettings(v map[string]string) []string {
	var errs []string
	if v["blogtitle"] == "" {
		errs = append(errs, "Your blog must have a title.")
	}
	for _, e := range []struct{ key, label string }{
		{"owneremail", "owner email"},
		{"failto", "fail to"},
		{"commentsfrom", "comments sent from"},
	} {
		if v[e.key] != "" && !validEmail(v[e.key]) {
			errs = append(errs, "The "+e.label+" setting must be a valid email address.")
		}
	}
	for _, n := range []struct{ key, label string }{
		{"maxentries", "Max entries"},
		{"maxentriesadmin", "Max entries admin"},
	} {
		count, err := strconv.Atoi(v[n.key])
		if err != nil || count < 1 {
			errs = append(errs, n.label+" must be a whole number of 1 or more.")
		}
	}
	switch tz := v["timezone"]; {
	case tz == "":
		errs = append(errs, "The time zone cannot be blank: use an IANA zone such as UTC.")
	default:
		if _, err := time.LoadLocation(tz); err != nil {
			errs = append(errs, "The time zone "+tz+" is not a zone this host knows.")
		}
	}
	if !i18n.Known(v["locale"]) {
		errs = append(errs, "The locale "+v["locale"]+" is not one this blog has strings for.")
	}
	return errs
}
