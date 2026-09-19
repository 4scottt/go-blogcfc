package auth_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/4scottt/go-blogcfc/internal/auth"
	"github.com/4scottt/go-blogcfc/internal/store"
)

const testSecret = "a-test-session-secret"

// fakeUsers is a UserSource with no database: sessions do not need one,
// and the admin's handler tests cover the real store.
type fakeUsers map[string]*store.User

func (f fakeUsers) GetUser(_ context.Context, username string) (*store.User, error) {
	u, ok := f[username]
	if !ok {
		return nil, store.ErrNotFound
	}
	return u, nil
}

func userWithRoles(name string, roles ...string) *store.User {
	u := &store.User{Username: name, Name: name}
	for i, r := range roles {
		u.Roles = append(u.Roles, store.Role{ID: i + 1, Role: r})
	}
	return u
}

// signCookie builds a cookie value the way the manager does, so a test
// can forge one with an issue time of its choosing.
func signCookie(t *testing.T, secret, username string, issued time.Time) string {
	t.Helper()
	payload := username + "|" + strconv.FormatInt(issued.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	if _, err := mac.Write([]byte(payload)); err != nil {
		t.Fatalf("hmac: %v", err)
	}
	return payload + "|" + hex.EncodeToString(mac.Sum(nil))
}

// loginCookie logs a user in and returns the cookie that came back.
func loginCookie(t *testing.T, m *auth.Manager, username string) *http.Cookie {
	t.Helper()
	rec := httptest.NewRecorder()
	m.Login(rec, username)
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.CookieName {
			return c
		}
	}
	t.Fatalf("Login set no %s cookie", auth.CookieName)
	return nil
}

// requestWith returns a GET carrying the cookie value.
func requestWith(value string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	if value != "" {
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: value})
	}
	return r
}

// TestFP_O06_SessionCookieSignedHttpOnlySecureAndTamperingLogsOut covers
// PLAN §9 O06: the cookie's attributes, Secure following the base URL's
// scheme, and a tampered, expired or orphaned cookie reading as no user.
func TestFP_O06_SessionCookieSignedHttpOnlySecureAndTamperingLogsOut(t *testing.T) {
	users := fakeUsers{"admin": userWithRoles("admin", "Admin")}
	m := auth.New(testSecret, false, users)

	c := loginCookie(t, m, "admin")
	if !c.HttpOnly {
		t.Error("cookie is not HttpOnly")
	}
	if c.Secure {
		t.Error("cookie is Secure although the manager was built with secure=false")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", c.SameSite)
	}
	if c.Path != "/" {
		t.Errorf("Path = %q, want /", c.Path)
	}
	if want := int(auth.SessionLifetime / time.Second); c.MaxAge != want {
		t.Errorf("MaxAge = %d, want %d (30 days)", c.MaxAge, want)
	}
	if parts := strings.Split(c.Value, "|"); len(parts) != 3 || parts[0] != "admin" {
		t.Fatalf("cookie value %q is not username|issued|signature", c.Value)
	}

	// The happy path: a signed cookie names the user.
	if u := m.Current(requestWith(c.Value)); u == nil || u.Username != "admin" {
		t.Fatalf("Current with a fresh cookie = %v, want admin", u)
	}

	// A flipped byte in the signature logs the request out.
	tampered := []byte(c.Value)
	last := len(tampered) - 1
	if tampered[last] == 'a' {
		tampered[last] = 'b'
	} else {
		tampered[last] = 'a'
	}
	if u := m.Current(requestWith(string(tampered))); u != nil {
		t.Error("a tampered signature still authenticated")
	}

	// So does a changed username, a missing cookie and a malformed one.
	forUser := "root|" + strings.Join(strings.Split(c.Value, "|")[1:], "|")
	if u := m.Current(requestWith(forUser)); u != nil {
		t.Error("a swapped username still authenticated")
	}
	if u := m.Current(requestWith("")); u != nil {
		t.Error("no cookie still authenticated")
	}
	if u := m.Current(requestWith("nonsense")); u != nil {
		t.Error("a malformed cookie still authenticated")
	}

	// A correctly signed cookie older than the lifetime is expired.
	old := signCookie(t, testSecret, "admin", time.Now().Add(-auth.SessionLifetime-time.Hour))
	if u := m.Current(requestWith(old)); u != nil {
		t.Error("a 30-day-old cookie still authenticated")
	}

	// A cookie signed with another secret is not ours.
	other := signCookie(t, "another-secret", "admin", time.Now())
	if u := m.Current(requestWith(other)); u != nil {
		t.Error("a cookie signed with a different secret authenticated")
	}

	// A valid cookie for a user who has since been deleted is no user.
	gone := signCookie(t, testSecret, "ghost", time.Now())
	if u := m.Current(requestWith(gone)); u != nil {
		t.Error("a cookie for a deleted user authenticated")
	}

	// Logout clears the cookie.
	rec := httptest.NewRecorder()
	m.Logout(rec)
	cleared := rec.Result().Cookies()
	if len(cleared) != 1 || cleared[0].Name != auth.CookieName || cleared[0].Value != "" || cleared[0].MaxAge >= 0 {
		t.Fatalf("Logout cookie = %+v, want an empty %s with a negative MaxAge", cleared, auth.CookieName)
	}

	// Secure follows the flag, which the caller derives from https.
	secureMgr := auth.New(testSecret, true, users)
	if c := loginCookie(t, secureMgr, "admin"); !c.Secure {
		t.Error("cookie is not Secure although the base URL is https")
	}
}

// TestFP_O06_RequireLoginRedirectsAndCachesTheUser: the gate sends a
// signed-out request to the login with a return path, and a signed-in one
// reaches the handler with the user in its context, loaded once.
func TestFP_O06_RequireLoginRedirectsAndCachesTheUser(t *testing.T) {
	users := &countingUsers{fakeUsers{"admin": userWithRoles("admin", "Admin")}, 0}
	m := auth.New(testSecret, false, users)

	var seen *store.User
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = auth.UserFrom(r.Context())
		// A handler asking again must not cost a second query.
		_ = m.Current(r)
		w.WriteHeader(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	m.RequireLogin(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/entries?sort=title", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("signed out: status %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/admin/login?return=%2Fadmin%2Fentries%3Fsort%3Dtitle" {
		t.Fatalf("Location = %q, want the login with the return path", loc)
	}

	c := loginCookie(t, m, "admin")
	rec = httptest.NewRecorder()
	m.RequireLogin(next).ServeHTTP(rec, requestWith(c.Value))
	if rec.Code != http.StatusOK {
		t.Fatalf("signed in: status %d, want 200", rec.Code)
	}
	if seen == nil || seen.Username != "admin" {
		t.Fatalf("handler saw %v in the context, want admin", seen)
	}
	if users.calls != 1 {
		t.Errorf("GetUser called %d times in one request, want 1", users.calls)
	}
}

// countingUsers counts the lookups, so the context cache is provable.
type countingUsers struct {
	fakeUsers
	calls int
}

func (c *countingUsers) GetUser(ctx context.Context, username string) (*store.User, error) {
	c.calls++
	return c.fakeUsers.GetUser(ctx, username)
}
