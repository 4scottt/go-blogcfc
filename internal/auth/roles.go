package auth

import "github.com/4scottt/go-blogcfc/internal/store"

// The role names BlogCFC's installer seeds (internal/migrate/seed.go) and
// the one the as-is admin menu checks for but never seeds, PageAdmin: its
// screens are reachable only through Admin until a later package adds the
// role (PLAN §9 A19).
const (
	RoleAdmin            = "Admin"
	RoleAddCategory      = "AddCategory"
	RoleManageCategories = "ManageCategories"
	RoleManageUsers      = "ManageUsers"
	RoleReleaseEntries   = "ReleaseEntries"
	RolePageAdmin        = "PageAdmin"
)

// HasRole reports whether u holds role, with BlogCFC's rule that Admin
// implies all (isBlogAuthorized in org/camden/blog/blog.cfc). A nil user
// holds nothing, so a caller may pass the result of UserFrom directly.
func HasRole(u *store.User, role string) bool {
	if u == nil {
		return false
	}
	return u.HasRole(role)
}
