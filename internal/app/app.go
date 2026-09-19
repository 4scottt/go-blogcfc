// Package app wires the process together: the router, the middleware and
// the routes that belong to no feature package. Feature packages register
// their own routes as Modules.
package app

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/4scottt/go-blogcfc/internal/config"
	"github.com/4scottt/go-blogcfc/internal/static"
	"github.com/4scottt/go-blogcfc/internal/store"
)

// Module is how a later package adds its handlers to the one mux.
type Module interface {
	Routes(mux *http.ServeMux)
}

// healthTimeout bounds the database ping behind GET /health. The platform
// probes often; a hung database must answer 503 quickly, not block.
const healthTimeout = 2 * time.Second

// App holds what the handlers share.
type App struct {
	cfg      *config.Config
	store    *store.Store
	settings *config.Settings
}

// New builds the handler: the mux with /health and /static/, every
// module's routes, wrapped in request logging and panic recovery.
func New(cfg *config.Config, st *store.Store, settings *config.Settings, modules ...Module) http.Handler {
	a := &App{cfg: cfg, store: st, settings: settings}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", a.handleHealth)

	// static.FS is rooted at the asset tree, so css/site.css answers
	// /static/css/site.css.
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static.FS)))

	for _, m := range modules {
		m.Routes(mux)
	}
	return recoverPanic(logRequests(mux))
}

// Config, Store and Settings let modules reach the shared dependencies
// without a second wiring path.
func (a *App) Config() *config.Config     { return a.cfg }
func (a *App) Store() *store.Store        { return a.store }
func (a *App) Settings() *config.Settings { return a.settings }

// handleHealth is the platform's probe: no auth, no work beyond a short
// database ping (PLAN §6, FP O02).
func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), healthTimeout)
	defer cancel()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := a.store.Ping(ctx); err != nil {
		slog.Warn("health: database unreachable", "error", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("db unavailable"))
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}
