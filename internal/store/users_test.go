package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/4scottt/go-blogcfc/internal/auth"
	"github.com/4scottt/go-blogcfc/internal/store"
	"github.com/4scottt/go-blogcfc/internal/testdb"
)

// TestUserCRUDAndRolesRoundTrip: users, their roles and the rule
// that a blank password leaves the stored hash alone.
func TestUserCRUDAndRolesRoundTrip(t *testing.T) {
	st := testdb.New(t)
	ctx := context.Background()

	roles, err := st.ListRoles(ctx)
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	if len(roles) != 5 {
		t.Fatalf("roles = %d, want the five seeds", len(roles))
	}
	byName := map[string]int{}
	for _, r := range roles {
		byName[r.Role] = r.ID
	}
	for _, want := range []string{"AddCategory", "ManageCategories", "Admin", "ManageUsers", "ReleaseEntries"} {
		if _, ok := byName[want]; !ok {
			t.Fatalf("role %q is missing", want)
		}
	}

	hash, err := auth.HashPassword("s3cret")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	u := &store.User{Username: "editor", PasswordHash: hash, Name: "An Editor"}
	if err := st.CreateUser(ctx, u, []int{byName["AddCategory"], byName["ReleaseEntries"], byName["AddCategory"]}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if len(u.Roles) != 2 {
		t.Fatalf("roles after create = %+v, want two (the duplicate collapsed)", u.Roles)
	}

	if err := st.CreateUser(ctx, &store.User{Username: "editor"}, nil); !errors.Is(err, store.ErrDuplicate) {
		t.Fatalf("a duplicate username gave %v, want ErrDuplicate", err)
	}

	got, err := st.GetUser(ctx, "editor")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if got.Name != "An Editor" || !auth.CheckPassword(got.PasswordHash, "s3cret") {
		t.Fatalf("GetUser returned %+v", got)
	}
	if !got.HasRole("ReleaseEntries") || got.HasRole("ManageUsers") {
		t.Fatalf("role checks: %+v", got.Roles)
	}
	if _, err := st.GetUser(ctx, "nobody"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing user gave %v, want ErrNotFound", err)
	}

	// A blank hash means "do not touch the password"; the roles are replaced.
	got.PasswordHash = ""
	got.Name = "Renamed"
	if err := st.UpdateUser(ctx, got, []int{byName["Admin"]}); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	reread, err := st.GetUser(ctx, "editor")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if reread.Name != "Renamed" || !auth.CheckPassword(reread.PasswordHash, "s3cret") {
		t.Fatalf("a blank hash changed the password: %+v", reread)
	}
	if len(reread.Roles) != 1 || reread.Roles[0].Role != "Admin" {
		t.Fatalf("roles after the update = %+v", reread.Roles)
	}
	if !reread.HasRole("ManageUsers") {
		t.Error("Admin should imply every role")
	}

	newHash, _ := auth.HashPassword("changed")
	reread.PasswordHash = newHash
	if err := st.UpdateUser(ctx, reread, []int{byName["Admin"]}); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	after, _ := st.GetUser(ctx, "editor")
	if !auth.CheckPassword(after.PasswordHash, "changed") {
		t.Fatal("a non-empty hash did not replace the password")
	}

	users, err := st.ListUsers(ctx)
	if err != nil || len(users) != 1 || len(users[0].Roles) != 1 {
		t.Fatalf("ListUsers = %+v (err %v)", users, err)
	}
	if n, err := st.CountUsers(ctx); err != nil || n != 1 {
		t.Fatalf("CountUsers = %d (err %v)", n, err)
	}

	if err := st.DeleteUser(ctx, "editor"); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, err := st.GetUser(ctx, "editor"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("the user survived the delete: %v", err)
	}
	var left int
	if err := st.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM user_roles WHERE username = ?", "editor").Scan(&left); err != nil {
		t.Fatalf("count user_roles: %v", err)
	}
	if left != 0 {
		t.Fatalf("role rows left behind: %d", left)
	}
}

// TestPasswordHashingIsBcrypt keeps the hashing contract explicit.
func TestPasswordHashingIsBcrypt(t *testing.T) {
	h1, err := auth.HashPassword("hunter2")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	h2, _ := auth.HashPassword("hunter2")
	if h1 == h2 {
		t.Error("two hashes of the same password are identical: the salt is missing")
	}
	if len(h1) < 55 || h1[:4] != "$2a$" {
		t.Errorf("hash %q does not look like bcrypt", h1)
	}
	if !auth.CheckPassword(h1, "hunter2") {
		t.Error("the right password was refused")
	}
	if auth.CheckPassword(h1, "hunter3") {
		t.Error("the wrong password was accepted")
	}
	if auth.CheckPassword("", "hunter2") {
		t.Error("an empty hash accepted a password")
	}
}
