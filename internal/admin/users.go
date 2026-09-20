package admin

import (
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/4scottt/go-blogcfc/internal/auth"
	"github.com/4scottt/go-blogcfc/internal/store"
)

// usernamePattern is what a username may contain. The as-is validates
// nothing beyond "not blank" (admin/user.cfm) and then puts the name in
// URLs and mail; the rewrite keeps it to the characters that are safe in
// a path segment (PLAN §9 A17).
var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// usersPage is users.html's own data.
type usersPage struct {
	Rows []userRow
}

// userRow is one line of the users table: the three columns the as-is
// datatable showed, plus the roles the rewrite's list adds.
type userRow struct {
	Username string
	Name     string
	Roles    string
}

// usersList is GET /admin/users (PLAN §9 A17, admin/users.cfm).
func (m *Module) usersList(w http.ResponseWriter, r *http.Request) {
	users, err := m.store.ListUsers(r.Context())
	if err != nil {
		m.serverError(w, r, err)
		return
	}
	rows := make([]userRow, 0, len(users))
	for _, u := range users {
		names := make([]string, 0, len(u.Roles))
		for _, role := range u.Roles {
			names = append(names, role.Role)
		}
		rows = append(rows, userRow{Username: u.Username, Name: u.Name, Roles: strings.Join(names, ", ")})
	}
	data := m.newPageData(r, "Users")
	data.Flash = usersFlash(r)
	data.Page = usersPage{Rows: rows}
	render(w, "users.html", data)
}

// usersFlash is the banner a redirect asks for.
func usersFlash(r *http.Request) string {
	q := r.URL.Query()
	switch {
	case q.Has("saved"):
		return "User saved."
	case q.Has("deleted"):
		return "User deleted."
	case q.Has("self"):
		return "You cannot delete the account you are signed in with."
	default:
		return ""
	}
}

// roleChoice is one option of the roles multiselect.
type roleChoice struct {
	ID          int
	Role        string
	Description string
	Selected    bool
}

// userFormPage is user.html's own data. Username is shown but not
// editable once the user exists: the as-is prints it as text on an edit
// and only offers an input on a create (admin/user.cfm).
type userFormPage struct {
	IsNew    bool
	Action   string
	Errors   []string
	Username string
	Name     string
	Roles    []roleChoice
	// CanDelete is false on a create and on your own account.
	CanDelete    bool
	DeleteAction string
}

// userForm is GET /admin/users/{name} and /admin/users/new.
func (m *Module) userForm(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	roles, err := m.store.ListRoles(ctx)
	if err != nil {
		m.serverError(w, r, err)
		return
	}

	name := userPathName(r)
	p := userFormPage{IsNew: name == "", Action: "/admin/users/new"}
	selected := map[int]bool{}
	if !p.IsNew {
		u, err := m.store.GetUser(ctx, name)
		if errors.Is(err, store.ErrNotFound) {
			m.notFound(w, r)
			return
		}
		if err != nil {
			m.serverError(w, r, err)
			return
		}
		p.Username, p.Name = u.Username, u.Name
		p.Action = "/admin/users/" + u.Username
		p.DeleteAction = p.Action + "/delete"
		p.CanDelete = !m.isSelf(r, u.Username)
		for _, role := range u.Roles {
			selected[role.ID] = true
		}
	}
	p.Roles = roleChoices(roles, selected)

	data := m.newPageData(r, "User Editor")
	data.Page = p
	render(w, "user.html", data)
}

// userSave is POST /admin/users/{name} and /admin/users/new. A refused
// save re-renders the form with what was typed and why (PLAN §9 A17).
func (m *Module) userSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	allRoles, err := m.store.ListRoles(ctx)
	if err != nil {
		m.serverError(w, r, err)
		return
	}

	name := userPathName(r)
	var existing *store.User
	if name != "" {
		u, err := m.store.GetUser(ctx, name)
		if errors.Is(err, store.ErrNotFound) {
			m.notFound(w, r)
			return
		}
		if err != nil {
			m.serverError(w, r, err)
			return
		}
		existing = u
	}

	p := userFormPage{
		IsNew:  existing == nil,
		Action: "/admin/users/new",
		Name:   strings.TrimSpace(r.PostFormValue("name")),
	}
	// The username of an existing user comes from the path, never from
	// the form: it is the primary key and the form only shows it.
	if existing != nil {
		p.Username = existing.Username
		p.Action = "/admin/users/" + existing.Username
		p.DeleteAction = p.Action + "/delete"
		p.CanDelete = !m.isSelf(r, existing.Username)
	} else {
		p.Username = strings.TrimSpace(r.PostFormValue("username"))
	}
	password := r.PostFormValue("password")

	roleIDs, selected, badRole := parseRoleIDs(r.PostForm["roles"], allRoles)
	p.Roles = roleChoices(allRoles, selected)

	var errs []string
	switch {
	case p.Username == "":
		errs = append(errs, "The username cannot be blank.")
	case len([]rune(p.Username)) > 50:
		errs = append(errs, "The username may be at most 50 characters.")
	case !usernamePattern.MatchString(p.Username):
		errs = append(errs, "The username may only contain letters, numbers, dots, hyphens and underscores.")
	}
	switch {
	case p.Name == "":
		errs = append(errs, "The name cannot be blank.")
	case len([]rune(p.Name)) > 100:
		errs = append(errs, "The name may be at most 100 characters.")
	}
	// A blank password on an edit means "leave it alone", which is the
	// as-is passwordCheck trick without the round trip of the hash
	// through the browser (PLAN §9 A17).
	if existing == nil && strings.TrimSpace(password) == "" {
		errs = append(errs, "The password cannot be blank.")
	}
	if badRole {
		errs = append(errs, "One of the roles submitted does not exist.")
	}

	if len(errs) == 0 && existing == nil {
		if _, err := m.store.GetUser(ctx, p.Username); err == nil {
			errs = append(errs, "A user with this username already exists.")
		} else if !errors.Is(err, store.ErrNotFound) {
			m.serverError(w, r, err)
			return
		}
	}

	if len(errs) == 0 {
		u := &store.User{Username: p.Username, Name: p.Name}
		if strings.TrimSpace(password) != "" {
			hash, err := auth.HashPassword(password)
			if err != nil {
				m.serverError(w, r, err)
				return
			}
			u.PasswordHash = hash
		}
		if existing != nil {
			err = m.store.UpdateUser(ctx, u, roleIDs)
		} else {
			err = m.store.CreateUser(ctx, u, roleIDs)
		}
		switch {
		case errors.Is(err, store.ErrDuplicate):
			errs = append(errs, "A user with this username already exists.")
		case errors.Is(err, store.ErrNotFound):
			m.notFound(w, r)
			return
		case err != nil:
			m.serverError(w, r, err)
			return
		default:
			m.flush()
			http.Redirect(w, r, "/admin/users?saved=1", http.StatusFound)
			return
		}
	}

	p.Errors = errs
	data := m.newPageData(r, "User Editor")
	data.Page = p
	render(w, "user.html", data)
}

// userDelete is POST /admin/users/{name}/delete. Deleting the account you
// are signed in with locks you out of the blog, so the screen refuses it;
// the as-is only warned about it in prose (admin/users.cfm).
func (m *Module) userDelete(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" || name == "new" {
		m.notFound(w, r)
		return
	}
	if m.isSelf(r, name) {
		http.Redirect(w, r, "/admin/users?self=1", http.StatusFound)
		return
	}
	if err := m.store.DeleteUser(r.Context(), name); err != nil {
		m.serverError(w, r, err)
		return
	}
	m.flush()
	http.Redirect(w, r, "/admin/users?deleted=1", http.StatusFound)
}

// isSelf reports whether username is the signed-in user's own.
func (m *Module) isSelf(r *http.Request, username string) bool {
	u := m.sessions.Current(r)
	return u != nil && strings.EqualFold(u.Username, username)
}

// roleChoices turns the role table into the multiselect's options.
func roleChoices(roles []store.Role, selected map[int]bool) []roleChoice {
	out := make([]roleChoice, 0, len(roles))
	for _, r := range roles {
		out = append(out, roleChoice{ID: r.ID, Role: r.Role, Description: r.Description, Selected: selected[r.ID]})
	}
	return out
}

// parseRoleIDs reads the multiselect. An id that is not a role is
// reported rather than dropped, so a broken form is not a silent
// demotion.
func parseRoleIDs(raw []string, roles []store.Role) (ids []int, selected map[int]bool, bad bool) {
	known := map[int]bool{}
	for _, r := range roles {
		known[r.ID] = true
	}
	selected = map[int]bool{}
	for _, v := range raw {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		id, err := strconv.Atoi(v)
		if err != nil || !known[id] {
			bad = true
			continue
		}
		if selected[id] {
			continue
		}
		selected[id] = true
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids, selected, bad
}

// userPathName reads {name}, mapping `new` to the empty string as the
// other editors map their `new` id.
func userPathName(r *http.Request) string {
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "new" {
		return ""
	}
	return name
}
