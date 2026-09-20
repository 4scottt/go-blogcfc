package admin_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/4scottt/go-blogcfc/internal/auth"
	"github.com/4scottt/go-blogcfc/internal/store"
)

// roleID is the id of a seeded role, by name.
func (h *harness) roleID(name string) string {
	h.t.Helper()
	roles, err := h.store.ListRoles(context.Background())
	if err != nil {
		h.t.Fatalf("ListRoles: %v", err)
	}
	for _, r := range roles {
		if strings.EqualFold(r.Role, name) {
			return strconv.Itoa(r.ID)
		}
	}
	h.t.Fatalf("role %q is not seeded", name)
	return ""
}

// storedUser reads a user straight from the database, to check what a
// handler did rather than what it said.
func (h *harness) storedUser(username string) *store.User {
	h.t.Helper()
	u, err := h.store.GetUser(context.Background(), username)
	if err != nil {
		h.t.Fatalf("GetUser(%s): %v", username, err)
	}
	return u
}

// TestFP_A17_UsersListCRUDImmutableUsernamePasswordOnlyWhenChangedRoles
// covers PLAN §9 A17: the list behind ManageUsers, create and edit, the
// username that cannot change, the password written only when one was
// typed, the roles multiselect, and the Delete form that is missing on
// your own account.
func TestFP_A17_UsersListCRUDImmutableUsernamePasswordOnlyWhenChangedRoles(t *testing.T) {
	h := newHarness(t)
	h.user("admin", "Admin")
	h.user("writer", "AddCategory")

	// The screen is behind ManageUsers (A19).
	h.login("writer")
	resp, _ := h.get("/admin/users")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("GET /admin/users as a user without ManageUsers: status %d, want 403", resp.StatusCode)
	}
	h.get("/admin/logout")

	h.login("admin")
	resp, body := h.get("/admin/users")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("users: status %d, want 200", resp.StatusCode)
	}
	for _, want := range []string{"<h1>Users</h1>", "Add User", "<th>Username</th>", "<th>Name</th>", "<th>Roles</th>", "writer"} {
		if !strings.Contains(body, want) {
			t.Errorf("the users list has no %q", want)
		}
	}

	// Create. The roles multiselect posts role ids.
	manageCats, releaseEntries := h.roleID("ManageCategories"), h.roleID("ReleaseEntries")
	resp, body = h.postForm("/admin/users/new", url.Values{
		"username": {"editor.one"}, "name": {"Editor One"},
		"password": {"first-password"}, "roles": {manageCats}, "save": {"Save"},
	})
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/admin/users?saved=1" {
		t.Fatalf("create: status %d to %q, want 302 to the list (body: %s)", resp.StatusCode, resp.Header.Get("Location"), body)
	}
	created := h.storedUser("editor.one")
	if created.Name != "Editor One" {
		t.Errorf("created name = %q, want %q", created.Name, "Editor One")
	}
	if !auth.CheckPassword(created.PasswordHash, "first-password") {
		t.Error("the created user's password was not stored as a usable hash")
	}
	if len(created.Roles) != 1 || created.Roles[0].Role != "ManageCategories" {
		t.Errorf("created roles = %+v, want ManageCategories only", created.Roles)
	}

	_, body = h.get("/admin/users")
	for _, want := range []string{"editor.one", "Editor One", "ManageCategories"} {
		if !strings.Contains(body, want) {
			t.Errorf("the list does not show %q", want)
		}
	}

	// The edit form shows the username but does not offer an input for it.
	_, body = h.get("/admin/users/editor.one")
	if !strings.Contains(body, "editor.one") {
		t.Error("the edit form does not show the username")
	}
	if strings.Contains(body, `name="username"`) {
		t.Error("the edit form offers an editable username")
	}
	for _, want := range []string{`name="name"`, `name="password"`, `name="roles"`, `value="Save"`, `value="Delete"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the edit form has no %s", want)
		}
	}

	// Edit with a blank password: name and roles change, the hash does not.
	before := created.PasswordHash
	resp, body = h.postForm("/admin/users/editor.one", url.Values{
		"username": {"someone.else"}, "name": {"Editor Renamed"},
		"password": {""}, "roles": {manageCats, releaseEntries}, "save": {"Save"},
	})
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("edit: status %d, want 302 (body: %s)", resp.StatusCode, body)
	}
	edited := h.storedUser("editor.one")
	if edited.PasswordHash != before {
		t.Error("a blank password field rewrote the password hash")
	}
	if edited.Name != "Editor Renamed" {
		t.Errorf("edited name = %q, want %q", edited.Name, "Editor Renamed")
	}
	if len(edited.Roles) != 2 {
		t.Errorf("edited roles = %+v, want two", edited.Roles)
	}
	if _, err := h.store.GetUser(context.Background(), "someone.else"); err == nil {
		t.Error("a username posted to an edit renamed the user")
	}

	// Edit with a password: the hash changes and the new one works.
	resp, _ = h.postForm("/admin/users/editor.one", url.Values{
		"name": {"Editor Renamed"}, "password": {"second-password"},
		"roles": {manageCats}, "save": {"Save"},
	})
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("password edit: status %d, want 302", resp.StatusCode)
	}
	repassworded := h.storedUser("editor.one")
	if repassworded.PasswordHash == before {
		t.Error("a typed password did not change the hash")
	}
	if !auth.CheckPassword(repassworded.PasswordHash, "second-password") {
		t.Error("the typed password is not the stored one")
	}

	// Validation: the pattern, the length and the duplicate.
	for _, bad := range []struct{ name, username, message string }{
		{"a space", "two words", "may only contain"},
		{"a slash", "a/b", "may only contain"},
		{"blank", "", "cannot be blank"},
		{"too long", strings.Repeat("a", 51), "at most 50"},
		{"duplicate", "editor.one", "already exists"},
	} {
		resp, body := h.postForm("/admin/users/new", url.Values{
			"username": {bad.username}, "name": {"Nope"}, "password": {"pw"}, "save": {"Save"},
		})
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s username: status %d, want 200 with errors", bad.name, resp.StatusCode)
			continue
		}
		if !strings.Contains(body, bad.message) {
			t.Errorf("%s username: the form does not say %q", bad.name, bad.message)
		}
	}
	// A create with no password is refused.
	_, body = h.postForm("/admin/users/new", url.Values{
		"username": {"nopassword"}, "name": {"No Password"}, "password": {""}, "save": {"Save"},
	})
	if !strings.Contains(body, "The password cannot be blank.") {
		t.Error("a create with a blank password was not refused")
	}
	if _, err := h.store.GetUser(context.Background(), "nopassword"); err == nil {
		t.Error("a refused create wrote a user anyway")
	}

	// Your own account carries no Delete form, and a hand-made delete is refused.
	_, body = h.get("/admin/users/admin")
	if strings.Contains(body, `value="Delete"`) {
		t.Error("the edit form offers to delete the account you are signed in with")
	}
	resp, _ = h.postForm("/admin/users/admin/delete", url.Values{"delete": {"Delete"}})
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/admin/users?self=1" {
		t.Fatalf("self delete: status %d to %q, want 302 to the list with ?self=1", resp.StatusCode, resp.Header.Get("Location"))
	}
	if _, err := h.store.GetUser(context.Background(), "admin"); err != nil {
		t.Fatalf("the signed-in account was deleted: %v", err)
	}

	// Another account goes.
	resp, _ = h.postForm("/admin/users/editor.one/delete", url.Values{"delete": {"Delete"}})
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/admin/users?deleted=1" {
		t.Fatalf("delete: status %d to %q, want 302 to the list with ?deleted=1", resp.StatusCode, resp.Header.Get("Location"))
	}
	if _, err := h.store.GetUser(context.Background(), "editor.one"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("after the delete, GetUser returned %v, want ErrNotFound", err)
	}
	_, body = h.get("/admin/users?deleted=1")
	if !strings.Contains(body, "User deleted.") {
		t.Error("the list shows no banner after a delete")
	}
}
