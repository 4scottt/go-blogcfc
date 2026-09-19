package admin

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/4scottt/go-blogcfc/internal/auth"
	"github.com/4scottt/go-blogcfc/internal/store"
)

// loginPage is login.html's own data.
type loginPage struct {
	// Message is the failure notice; empty on the first view. The as-is
	// says no more than "invalid", and neither do we: a message that
	// distinguished a bad username from a bad password would enumerate
	// users.
	Message string
	// Return is the path the login came from, carried through the form.
	Return string
}

// loginForm shows the form. A request that already has a session is sent
// on to the dashboard rather than asked to log in twice.
func (m *Module) loginForm(w http.ResponseWriter, r *http.Request) {
	if m.sessions.Current(r) != nil {
		http.Redirect(w, r, safeReturn(r.URL.Query().Get("return")), http.StatusFound)
		return
	}
	data := m.newPageData(r, "Login")
	data.Page = loginPage{Return: r.URL.Query().Get("return")}
	render(w, "login.html", data)
}

// loginSubmit checks the password. A failure sleeps 500 ms and re-renders
// the form with status 200, as the as-is does to slow brute force down
// (admin/Application.cfc, PLAN §9 A01).
func (m *Module) loginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	username := strings.TrimSpace(r.PostFormValue("username"))
	password := r.PostFormValue("password")
	ret := r.PostFormValue("return")
	if ret == "" {
		ret = r.URL.Query().Get("return")
	}

	if m.authenticate(r, username, password) {
		m.sessions.Login(w, username)
		http.Redirect(w, r, safeReturn(ret), http.StatusFound)
		return
	}

	time.Sleep(loginFailureDelay)
	data := m.newPageData(r, "Login")
	data.Page = loginPage{Message: "Invalid login", Return: ret}
	render(w, "login.html", data)
}

// authenticate reports whether the password matches. It hashes even when
// the user does not exist, so a missing user and a wrong password take
// about the same time.
func (m *Module) authenticate(r *http.Request, username, password string) bool {
	if username == "" || password == "" {
		return false
	}
	u, err := m.store.GetUser(r.Context(), username)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return false
		}
		// A hash of the right shape, so a missing user costs a bcrypt
		// comparison too.
		auth.CheckPassword("$2a$10$"+strings.Repeat("x", 53), password)
		return false
	}
	return auth.CheckPassword(u.PasswordHash, password)
}

// logout clears the cookie and returns to the login form.
func (m *Module) logout(w http.ResponseWriter, r *http.Request) {
	m.sessions.Logout(w)
	http.Redirect(w, r, auth.LoginPath, http.StatusFound)
}

// safeReturn keeps a `return` only when it is a same-site admin path:
// anything with a scheme or a host, or outside /admin, goes to the
// dashboard instead, so the login cannot be turned into an open redirect.
func safeReturn(raw string) string {
	if raw == "" {
		return adminHome
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "" || u.Host != "" || u.Opaque != "" {
		return adminHome
	}
	if !strings.HasPrefix(u.Path, "/admin") {
		return adminHome
	}
	return u.String()
}
