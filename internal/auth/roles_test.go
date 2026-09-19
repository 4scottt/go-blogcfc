package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/4scottt/go-blogcfc/internal/auth"
)

// TestFP_A19_RoleChecksAdminImpliesAll covers PLAN §9 A19: BlogCFC's
// isBlogAuthorized rule, and the 403 RequireRole answers with.
func TestFP_A19_RoleChecksAdminImpliesAll(t *testing.T) {
	everyRole := []string{
		auth.RoleAdmin, auth.RoleAddCategory, auth.RoleManageCategories,
		auth.RoleManageUsers, auth.RoleReleaseEntries, auth.RolePageAdmin,
	}

	admin := userWithRoles("admin", auth.RoleAdmin)
	for _, role := range everyRole {
		if !auth.HasRole(admin, role) {
			t.Errorf("Admin lacks %s, but Admin implies all", role)
		}
	}

	editor := userWithRoles("editor", auth.RoleAddCategory)
	if !auth.HasRole(editor, auth.RoleAddCategory) {
		t.Error("the editor lacks their own AddCategory role")
	}
	for _, role := range []string{auth.RoleManageUsers, auth.RoleManageCategories, auth.RoleReleaseEntries, auth.RoleAdmin} {
		if auth.HasRole(editor, role) {
			t.Errorf("the editor holds %s, which was never granted", role)
		}
	}
	if auth.HasRole(nil, auth.RoleAdmin) {
		t.Error("a nil user holds Admin")
	}

	// RequireRole: the editor is refused, the admin is let through.
	users := fakeUsers{"admin": admin, "editor": editor}
	m := auth.New(testSecret, false, users)
	reached := false
	guarded := m.RequireRole(auth.RoleManageUsers, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	guarded.ServeHTTP(rec, requestWith(loginCookie(t, m, "editor").Value))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("editor: status %d, want 403", rec.Code)
	}
	if reached {
		t.Fatal("the guarded handler ran for a user without the role")
	}

	rec = httptest.NewRecorder()
	guarded.ServeHTTP(rec, requestWith(loginCookie(t, m, "admin").Value))
	if rec.Code != http.StatusOK || !reached {
		t.Fatalf("admin: status %d, reached %v, want 200 and true", rec.Code, reached)
	}

	// Signed out, RequireRole redirects rather than showing a 403.
	rec = httptest.NewRecorder()
	guarded.ServeHTTP(rec, requestWith(""))
	if rec.Code != http.StatusFound {
		t.Fatalf("signed out: status %d, want 302 to the login", rec.Code)
	}
}
