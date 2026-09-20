package admin_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/4scottt/go-blogcfc/internal/auth"
)

// TestFP_A18_PasswordChangeChecksOldPassword covers PLAN §9 A18: the
// form's three fields, the old password that has to be right, the
// confirmation that has to match, and the new password that then works
// at the login (admin/updatepassword.cfm).
func TestFP_A18_PasswordChangeChecksOldPassword(t *testing.T) {
	h := newHarness(t)
	h.user("writer", "AddCategory")
	h.login("writer")

	resp, body := h.get("/admin/password")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("password: status %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "<h1>Update Password</h1>") {
		t.Error("the password page's heading is not Update Password")
	}
	for _, want := range []string{`name="oldpassword"`, `name="password"`, `name="password2"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the password form has no %s", want)
		}
	}

	original := h.storedUser("writer").PasswordHash

	// A wrong old password changes nothing.
	_, body = h.postForm("/admin/password", url.Values{
		"oldpassword": {"not-my-password"}, "password": {"a-new-password"},
		"password2": {"a-new-password"}, "update": {"Update"},
	})
	if !strings.Contains(body, "You entered the wrong old password.") {
		t.Error("a wrong old password was not reported")
	}
	if h.storedUser("writer").PasswordHash != original {
		t.Fatal("a wrong old password changed the stored hash")
	}

	// A mismatched confirmation changes nothing.
	_, body = h.postForm("/admin/password", url.Values{
		"oldpassword": {testPassword}, "password": {"a-new-password"},
		"password2": {"a-different-password"}, "update": {"Update"},
	})
	if !strings.Contains(body, "did not match") {
		t.Error("a mismatched confirmation was not reported")
	}
	if h.storedUser("writer").PasswordHash != original {
		t.Fatal("a mismatched confirmation changed the stored hash")
	}

	// A blank new password changes nothing.
	_, body = h.postForm("/admin/password", url.Values{
		"oldpassword": {testPassword}, "password": {""}, "password2": {""}, "update": {"Update"},
	})
	if !strings.Contains(body, "cannot be blank") {
		t.Error("a blank new password was not reported")
	}
	if h.storedUser("writer").PasswordHash != original {
		t.Fatal("a blank new password changed the stored hash")
	}

	// The real change.
	resp, body = h.postForm("/admin/password", url.Values{
		"oldpassword": {testPassword}, "password": {"a-new-password"},
		"password2": {"a-new-password"}, "update": {"Update"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("password change: status %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "Your password was updated.") {
		t.Error("a successful change says nothing")
	}
	changed := h.storedUser("writer")
	if changed.PasswordHash == original {
		t.Fatal("the stored hash did not change")
	}
	if !auth.CheckPassword(changed.PasswordHash, "a-new-password") {
		t.Fatal("the new password does not match the stored hash")
	}
	// The roles the user held travel through the write untouched.
	if len(changed.Roles) != 1 || changed.Roles[0].Role != "AddCategory" {
		t.Errorf("roles after the password change = %+v, want AddCategory only", changed.Roles)
	}

	// The new password is the one the login takes.
	h.get("/admin/logout")
	resp, _ = h.postLogin(url.Values{"username": {"writer"}, "password": {testPassword}, "login": {"Login"}})
	if resp.StatusCode != http.StatusOK {
		t.Errorf("the old password still logs in: status %d, want 200 with an error", resp.StatusCode)
	}
	resp, _ = h.postLogin(url.Values{"username": {"writer"}, "password": {"a-new-password"}, "login": {"Login"}})
	if resp.StatusCode != http.StatusFound {
		t.Errorf("the new password does not log in: status %d, want 302", resp.StatusCode)
	}
}
