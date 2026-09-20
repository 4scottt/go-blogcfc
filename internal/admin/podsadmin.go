package admin

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/4scottt/go-blogcfc/internal/config"
	"github.com/4scottt/go-blogcfc/internal/pods"
)

// knownPods is the fixed widget set the sidebar can draw, in pods.xml's
// own order. BlogCFC's pod manager listed whatever `.cfm` files were in
// includes/pods and let an operator upload and delete them; the rewrite
// ships the ten and keeps only the two things that screen really
// decided, show and order (PLAN §7 "Pod manager", §9 A23).
var knownPods = []struct {
	Name  string
	Label string
}{
	{pods.Calendar, "Calendar"},
	{pods.Subscribe, "Subscribe"},
	{pods.RecentComments, "Recent Comments"},
	{pods.Recent, "Recent Entries"},
	{pods.Archives, "Archives by Subject"},
	{pods.MonthlyArchives, "Monthly Archives"},
	{pods.Search, "Search"},
	{pods.TagCloud, "Tag Cloud"},
	{pods.RSS, "RSS Button"},
	{pods.Pages, "Pages"},
}

// podsPage is pods.html's own data.
type podsPage struct {
	Rows      []podRow
	Errors    []string
	Dropped   []string
	Defaulted bool
}

// podRow is one pod's line: the checkbox and the number beside its name.
type podRow struct {
	Name  string
	Label string
	Show  bool
	Order int
}

// podsForm is GET /admin/pods (PLAN §9 A23).
func (m *Module) podsForm(w http.ResponseWriter, r *http.Request) {
	stored := m.settings.Pods()
	p := podsPage{Defaulted: len(stored) == 0}
	if p.Defaulted {
		// An empty setting is the sidebar's own default list, so the
		// screen opens showing what a visitor actually sees (D10).
		stored = pods.Defaults()
	}
	p.Rows, p.Dropped = podRows(stored)
	data := m.newPageData(r, "Pods")
	data.Flash = savedDeletedFlash(r, "Pods")
	data.Page = p
	render(w, "pods.html", data)
}

// podRows merges the stored list onto the known set: a known pod keeps
// its stored show and order, a known pod the setting never mentioned
// comes after them, and a name that is not a pod any more is reported
// so the operator learns it was dropped rather than silently losing it.
func podRows(stored []config.Pod) ([]podRow, []string) {
	byName := make(map[string]config.Pod, len(stored))
	known := make(map[string]bool, len(knownPods))
	for _, p := range stored {
		byName[strings.ToLower(strings.TrimSpace(p.Name))] = p
	}
	rows := make([]podRow, 0, len(knownPods))
	next := len(stored) + 1
	for _, k := range knownPods {
		known[k.Name] = true
		row := podRow{Name: k.Name, Label: k.Label}
		if p, ok := byName[k.Name]; ok {
			row.Show, row.Order = p.Show, p.Order
		} else {
			row.Order = next
			next++
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Order < rows[j].Order })

	var dropped []string
	for _, p := range stored {
		if !known[strings.ToLower(strings.TrimSpace(p.Name))] {
			dropped = append(dropped, p.Name)
		}
	}
	return rows, dropped
}

// podsSave is POST /admin/pods: one `show_{name}` and one `order_{name}`
// per known pod, written back as the whole list in order. Anything the
// setting carried that is not one of the ten is dropped by writing only
// the ten.
func (m *Module) podsSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var (
		rows []podRow
		errs []string
	)
	for i, k := range knownPods {
		row := podRow{Name: k.Name, Label: k.Label, Show: r.PostFormValue("show_"+k.Name) != "", Order: i + 1}
		raw := strings.TrimSpace(r.PostFormValue("order_" + k.Name))
		if raw != "" {
			n, err := strconv.Atoi(raw)
			switch {
			case err != nil:
				errs = append(errs, "The sort order for "+k.Label+" has to be a number.")
			case n < 0:
				errs = append(errs, "The sort order for "+k.Label+" cannot be negative.")
			default:
				row.Order = n
			}
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Order < rows[j].Order })

	if len(errs) > 0 {
		data := m.newPageData(r, "Pods")
		data.Page = podsPage{Rows: rows, Errors: errs}
		render(w, "pods.html", data)
		return
	}

	list := make([]config.Pod, 0, len(rows))
	for _, row := range rows {
		list = append(list, config.Pod{Name: row.Name, Show: row.Show, Order: row.Order})
	}
	if err := m.settings.SetPods(r.Context(), list); err != nil {
		m.serverError(w, r, err)
		return
	}
	m.flush()
	m.reinit()
	http.Redirect(w, r, "/admin/pods?saved=1", http.StatusFound)
}
