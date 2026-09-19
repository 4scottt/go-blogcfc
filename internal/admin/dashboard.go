package admin

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/4scottt/go-blogcfc/internal/store"
)

// dashboardPage is dashboard.html's own data.
type dashboardPage struct {
	Version    string
	TopEntries []store.Entry
}

// dashboard is /admin/: the version, the most-viewed entries of the last
// seven days and, after ?reinit=1, the cache banner (PLAN §9 A02).
func (m *Module) dashboard(w http.ResponseWriter, r *http.Request) {
	data := m.newPageData(r, "Welcome to BlogCFC Administrator")

	// The as-is treats the presence of `reinit` as the trigger, whatever
	// its value (admin/index.cfm).
	if r.URL.Query().Has("reinit") {
		m.reinit()
		data.Flash = "Caches reinitialized"
	}

	top, err := m.store.TopEntriesByViews(r.Context(), time.Now().Add(-topEntriesWindow), 5)
	if err != nil {
		m.serverError(w, r, err)
		return
	}
	data.Page = dashboardPage{Version: Version, TopEntries: top}
	render(w, "dashboard.html", data)
}

// errorPage is error.html's own data.
type errorPage struct {
	Message string
}

// notFound answers an unknown /admin/ path for a signed-in user. Everyone
// else has already been redirected by the gate in Routes.
func (m *Module) notFound(w http.ResponseWriter, r *http.Request) {
	data := m.newPageData(r, "Not found")
	data.Page = errorPage{Message: "There is no admin page at " + r.URL.Path + "."}
	renderStatus(w, http.StatusNotFound, "error.html", data)
}

// forbidden is the body RequireRole shows a signed-in user who lacks the
// role; the module hands it to the session manager in New.
func (m *Module) forbidden(w http.ResponseWriter, r *http.Request) {
	data := m.newPageData(r, "Forbidden")
	data.Page = errorPage{Message: "Your account does not have the role this page needs."}
	renderStatus(w, http.StatusForbidden, "error.html", data)
}

// serverError logs and shows the admin's own 500.
func (m *Module) serverError(w http.ResponseWriter, r *http.Request, err error) {
	logError(r, err)
	data := m.newPageData(r, "Error")
	data.Page = errorPage{Message: "Something went wrong. The log has the detail."}
	renderStatus(w, http.StatusInternalServerError, "error.html", data)
}

// logError writes the one line the operator needs; the page never shows
// the detail.
func logError(r *http.Request, err error) {
	slog.Error("admin: request failed", "method", r.Method, "path", r.URL.Path, "error", err)
}
