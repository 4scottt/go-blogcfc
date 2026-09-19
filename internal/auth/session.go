package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/4scottt/go-blogcfc/internal/store"
)

// CookieName is the single cookie the admin session uses. Its value is
// `username|issued_unix|hex(HMAC-SHA256(secret, username|issued_unix))`:
// stateless, so a redeploy does not log anyone out, and nothing about the
// session lives in a header (PLAN §6: Caddy strips Authorization).
const CookieName = "gbc_session"

// SessionLifetime is how long a cookie stays valid, measured from the
// issue time carried inside it.
const SessionLifetime = 30 * 24 * time.Hour

// LoginPath is where an unauthenticated request to a gated page lands.
const LoginPath = "/admin/login"

// UserSource is the slice of the store a session needs: the user with
// their roles, looked up once per request. *store.Store satisfies it.
type UserSource interface {
	GetUser(ctx context.Context, username string) (*store.User, error)
}

// Manager signs, reads and clears the session cookie and gates handlers.
// It is safe for concurrent use; the fields are set at construction.
type Manager struct {
	secret []byte
	secure bool
	users  UserSource

	// Forbidden renders the 403 body for RequireRole. The admin package
	// sets it so a refusal looks like the rest of the admin; a nil
	// Forbidden answers in plain text, which keeps this package free of
	// templates and of an import cycle.
	Forbidden http.Handler
}

// New builds a Manager. secure marks the cookie Secure, which the caller
// derives from the base URL's scheme (PLAN §9 O06).
func New(secret string, secure bool, users UserSource) *Manager {
	return &Manager{secret: []byte(secret), secure: secure, users: users}
}

// Login writes a fresh session cookie for username.
func (m *Manager) Login(w http.ResponseWriter, username string) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    m.encode(username, time.Now()),
		Path:     "/",
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(SessionLifetime / time.Second),
	})
}

// Logout clears the cookie. The session is stateless, so clearing the
// cookie is the whole of it.
func (m *Manager) Logout(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

// Current returns the signed-in user, or nil when the cookie is absent,
// malformed, tampered with, expired, or names a user who no longer
// exists. A user already in the request context (RequireLogin puts one
// there) is returned without a second query.
func (m *Manager) Current(r *http.Request) *store.User {
	if u := UserFrom(r.Context()); u != nil {
		return u
	}
	username, ok := m.verify(r)
	if !ok {
		return nil
	}
	u, err := m.users.GetUser(r.Context(), username)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			slog.Error("session: user lookup failed", "username", username, "error", err)
		}
		return nil
	}
	return u
}

// RequireLogin gates a handler: no user means 302 to the login page with
// the wanted path in `return`; a user is put in the request context so
// the handler and the layout read it without another query.
func (m *Manager) RequireLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := m.Current(r)
		if u == nil {
			RedirectToLogin(w, r)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), u)))
	})
}

// RequireRole gates a handler on a role (Admin implies all, PLAN §9 A19).
// It includes RequireLogin, so a signed-out request is redirected and
// only a signed-in request without the role sees the 403.
func (m *Manager) RequireRole(role string, next http.Handler) http.Handler {
	return m.RequireLogin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !HasRole(UserFrom(r.Context()), role) {
			m.forbid(w, r)
			return
		}
		next.ServeHTTP(w, r)
	}))
}

func (m *Manager) forbid(w http.ResponseWriter, r *http.Request) {
	if m.Forbidden != nil {
		m.Forbidden.ServeHTTP(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte("forbidden"))
}

// RedirectToLogin sends the browser to the login form, remembering where
// it was going.
func RedirectToLogin(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, LoginPath+"?return="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
}

// encode builds the cookie value for a username issued at a given time.
func (m *Manager) encode(username string, issued time.Time) string {
	payload := username + "|" + strconv.FormatInt(issued.Unix(), 10)
	return payload + "|" + m.sign(payload)
}

// verify checks the cookie's signature and age and returns the username.
func (m *Manager) verify(r *http.Request) (string, bool) {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return "", false
	}
	parts := strings.Split(c.Value, "|")
	if len(parts) != 3 {
		return "", false
	}
	username, issuedRaw, sig := parts[0], parts[1], parts[2]
	if username == "" {
		return "", false
	}
	want := m.sign(username + "|" + issuedRaw)
	if !hmac.Equal([]byte(sig), []byte(want)) {
		return "", false
	}
	issuedUnix, err := strconv.ParseInt(issuedRaw, 10, 64)
	if err != nil {
		return "", false
	}
	issued := time.Unix(issuedUnix, 0)
	// Expired, or issued in a future this process cannot have signed.
	if time.Since(issued) > SessionLifetime || issued.After(time.Now().Add(time.Hour)) {
		return "", false
	}
	return username, true
}

func (m *Manager) sign(payload string) string {
	mac := hmac.New(sha256.New, m.secret)
	_, _ = mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// userKey types the context key so nothing else can collide with it.
type userKey struct{}

// WithUser puts the signed-in user in a context.
func WithUser(ctx context.Context, u *store.User) context.Context {
	return context.WithValue(ctx, userKey{}, u)
}

// UserFrom returns the user a context carries, or nil.
func UserFrom(ctx context.Context) *store.User {
	u, _ := ctx.Value(userKey{}).(*store.User)
	return u
}
