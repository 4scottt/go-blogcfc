package admin

import (
	"net/http"
	"strings"

	"github.com/4scottt/go-blogcfc/internal/auth"
	"github.com/4scottt/go-blogcfc/internal/store"
)

// passwordPage is password.html's own data. Nothing typed is ever sent
// back: a refused change re-renders empty fields.
type passwordPage struct {
	Errors []string
	Done   bool
}

// passwordForm is GET /admin/password (PLAN §9 A18,
// admin/updatepassword.cfm).
func (m *Module) passwordForm(w http.ResponseWriter, r *http.Request) {
	m.renderPassword(w, r, passwordPage{})
}

// passwordSave is POST /admin/password: the old password is checked
// against the stored hash, the two new ones must match, and only then is
// a new hash written. The as-is did the same through
// blog.updatePassword, which returned false on a wrong old password.
func (m *Module) passwordSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	u := m.sessions.Current(r)
	if u == nil {
		auth.RedirectToLogin(w, r)
		return
	}

	old := r.PostFormValue("oldpassword")
	// `password`/`password2` are the rewrite's names (PLAN §8 form
	// rules); the as-is called them newpassword/newpassword2 and those
	// are still accepted, so a bookmarked script or an operator's muscle
	// memory is not a silent no-op.
	next := firstNonEmpty(r.PostFormValue("password"), r.PostFormValue("newpassword"))
	confirm := firstNonEmpty(r.PostFormValue("password2"), r.PostFormValue("newpassword2"))

	var p passwordPage
	if strings.TrimSpace(old) == "" {
		p.Errors = append(p.Errors, "You must enter your old password.")
	}
	if strings.TrimSpace(next) == "" {
		p.Errors = append(p.Errors, "Your new password cannot be blank.")
	}
	if next != confirm {
		p.Errors = append(p.Errors, "Your new password and the confirmation did not match.")
	}
	if len(p.Errors) == 0 && !auth.CheckPassword(u.PasswordHash, old) {
		p.Errors = append(p.Errors, "You entered the wrong old password.")
	}
	if len(p.Errors) > 0 {
		m.renderPassword(w, r, p)
		return
	}

	hash, err := auth.HashPassword(next)
	if err != nil {
		m.serverError(w, r, err)
		return
	}
	// UpdateUser rewrites the role set, so the user's own roles travel
	// with the hash: a password change is not a change of access.
	ids := make([]int, 0, len(u.Roles))
	for _, role := range u.Roles {
		ids = append(ids, role.ID)
	}
	updated := &store.User{Username: u.Username, Name: u.Name, PasswordHash: hash}
	if err := m.store.UpdateUser(r.Context(), updated, ids); err != nil {
		m.serverError(w, r, err)
		return
	}
	m.renderPassword(w, r, passwordPage{Done: true})
}

func (m *Module) renderPassword(w http.ResponseWriter, r *http.Request, p passwordPage) {
	data := m.newPageData(r, "Update Password")
	data.Page = p
	render(w, "password.html", data)
}

// firstNonEmpty returns the first of its arguments that is not blank.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
