// Package admin is the logged-in side of the blog: the session-gated
// screens under /admin/ (PLAN §8 "Admin"). This package owns the admin
// frame — layout, menu, the login and the dashboard; the entry, category,
// comment and settings screens are later packages that reuse `render`
// and `pageData` and add their routes in routes.go.
package admin

import (
	"net/http"
	"time"

	"github.com/4scottt/go-blogcfc/internal/auth"
	"github.com/4scottt/go-blogcfc/internal/config"
	"github.com/4scottt/go-blogcfc/internal/store"
)

// Version is what the dashboard reports; the image tag and the card's
// prelude read the same string.
const Version = "0.1.0-dev"

// adminHome is where a login with no usable `return` lands.
const adminHome = "/admin/"

// loginFailureDelay slows brute force down, as the as-is does with a
// 500 ms Thread.sleep on a failed password (admin/Application.cfc,
// PLAN §9 A01). It is a var so a later test can shorten it.
var loginFailureDelay = 500 * time.Millisecond

// topEntriesWindow is the dashboard's "past seven days".
const topEntriesWindow = 7 * 24 * time.Hour

// Module is the admin's handlers and their dependencies.
type Module struct {
	cfg      *config.Config
	store    *store.Store
	settings *config.Settings
	sessions *auth.Manager

	// Reinit is called when the dashboard is asked for ?reinit=1. The
	// caches it flushes arrive with the render package, so this stays a
	// hook and a nil Reinit is a no-op (PLAN §9 A29).
	Reinit func()
}

// New builds the module. It also gives the session manager the admin's
// own 403 body, so a role refusal looks like the rest of the admin.
func New(cfg *config.Config, st *store.Store, settings *config.Settings, sessions *auth.Manager) *Module {
	m := &Module{cfg: cfg, store: st, settings: settings, sessions: sessions}
	sessions.Forbidden = http.HandlerFunc(m.forbidden)
	return m
}

// reinit runs the cache hook when one is set.
func (m *Module) reinit() {
	if m.Reinit != nil {
		m.Reinit()
	}
}
