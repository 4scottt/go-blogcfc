package admin

import (
	"net/http"

	"github.com/4scottt/go-blogcfc/internal/auth"
)

// Routes registers the admin's handlers. Keep this a plain list: a later
// package adds its entries, categories and settings lines here, and the
// gate each screen sits behind should be readable at a glance.
//
// The catch-all `GET /admin/` is the gate for everything not listed: an
// unknown admin path answers 404 to a signed-in user and 302 to the login
// to everyone else, so no future screen can be added outside the gate by
// accident (PLAN §9 A01).
func (m *Module) Routes(mux *http.ServeMux) {
	mux.Handle("GET /admin/login", http.HandlerFunc(m.loginForm))
	mux.Handle("POST /admin/login", http.HandlerFunc(m.loginSubmit))
	mux.Handle("GET /admin/logout", http.HandlerFunc(m.logout))
	mux.Handle("POST /admin/logout", http.HandlerFunc(m.logout))

	mux.Handle("GET /admin/{$}", m.sessions.RequireLogin(http.HandlerFunc(m.dashboard)))

	// Entries. `new` is a literal, so it wins over {id} and the handlers
	// treat both spellings as "no id yet" (PLAN §9 A03, A05, A06).
	mux.Handle("GET /admin/entries", m.sessions.RequireLogin(http.HandlerFunc(m.entriesList)))
	mux.Handle("POST /admin/entries/delete", m.sessions.RequireLogin(http.HandlerFunc(m.entriesDelete)))
	mux.Handle("GET /admin/entries/new", m.sessions.RequireLogin(http.HandlerFunc(m.entryForm)))
	mux.Handle("POST /admin/entries/new", m.sessions.RequireLogin(http.HandlerFunc(m.entrySave)))
	mux.Handle("GET /admin/entries/{id}", m.sessions.RequireLogin(http.HandlerFunc(m.entryForm)))
	mux.Handle("POST /admin/entries/{id}", m.sessions.RequireLogin(http.HandlerFunc(m.entrySave)))

	// Categories, behind ManageCategories as the as-is is (PLAN §9 A12).
	mux.Handle("GET /admin/categories", m.sessions.RequireRole(auth.RoleManageCategories, http.HandlerFunc(m.categoriesList)))
	mux.Handle("GET /admin/categories/new", m.sessions.RequireRole(auth.RoleManageCategories, http.HandlerFunc(m.categoryForm)))
	mux.Handle("POST /admin/categories/new", m.sessions.RequireRole(auth.RoleManageCategories, http.HandlerFunc(m.categorySave)))
	mux.Handle("GET /admin/categories/{id}", m.sessions.RequireRole(auth.RoleManageCategories, http.HandlerFunc(m.categoryForm)))
	mux.Handle("POST /admin/categories/{id}", m.sessions.RequireRole(auth.RoleManageCategories, http.HandlerFunc(m.categorySave)))
	mux.Handle("POST /admin/categories/{id}/delete", m.sessions.RequireRole(auth.RoleManageCategories, http.HandlerFunc(m.categoryDelete)))

	mux.Handle("GET /admin/", m.sessions.RequireLogin(http.HandlerFunc(m.notFound)))
}
