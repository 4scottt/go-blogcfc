package admin

import (
	"net/http"

	"github.com/4scottt/go-blogcfc/internal/auth"
)

// routesUsers registers the users, password and settings screens
// (PLAN §9 A17, A18, A20). Users sits behind ManageUsers as the as-is
// does (admin/users.cfm, admin/user.cfm); the password page is every
// signed-in user's own, and the settings page is the as-is one, which
// gated on nothing but a login.
func (m *Module) routesUsers(mux *http.ServeMux) {
	users := func(h http.HandlerFunc) http.Handler {
		return m.sessions.RequireRole(auth.RoleManageUsers, h)
	}
	mux.Handle("GET /admin/users", users(m.usersList))
	mux.Handle("GET /admin/users/new", users(m.userForm))
	mux.Handle("POST /admin/users/new", users(m.userSave))
	mux.Handle("GET /admin/users/{name}", users(m.userForm))
	mux.Handle("POST /admin/users/{name}", users(m.userSave))
	mux.Handle("POST /admin/users/{name}/delete", users(m.userDelete))

	mux.Handle("GET /admin/password", m.sessions.RequireLogin(http.HandlerFunc(m.passwordForm)))
	mux.Handle("POST /admin/password", m.sessions.RequireLogin(http.HandlerFunc(m.passwordSave)))

	mux.Handle("GET /admin/settings", m.sessions.RequireLogin(http.HandlerFunc(m.settingsPageHandler)))
	mux.Handle("POST /admin/settings", m.sessions.RequireLogin(http.HandlerFunc(m.settingsSave)))
}
