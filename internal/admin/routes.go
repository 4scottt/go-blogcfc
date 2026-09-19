package admin

import "net/http"

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
	mux.Handle("GET /admin/", m.sessions.RequireLogin(http.HandlerFunc(m.notFound)))
}
