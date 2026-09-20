// Package app wires the process together: the router, the middleware and
// the routes that belong to no feature package. Feature packages register
// their own routes as Modules.
package app

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/4scottt/go-blogcfc/internal/config"
	"github.com/4scottt/go-blogcfc/internal/render"
	"github.com/4scottt/go-blogcfc/internal/static"
	"github.com/4scottt/go-blogcfc/internal/store"
	"github.com/4scottt/go-blogcfc/internal/telemetry"
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
// module's routes, wrapped in request logging and panic recovery, and all
// of that inside the OpenTelemetry HTTP handler.
//
// The instrumentation goes outermost on purpose: it then times the whole
// request as the client sees it, and a panic that recoverPanic turns into
// a 500 is recorded as a 500 rather than escaping the span. It costs
// nothing when telemetry is off -- the global providers are the API's noop
// ones until Setup installs real ones (PLAN §6).
func New(cfg *config.Config, st *store.Store, settings *config.Settings, modules ...Module) http.Handler {
	a := &App{cfg: cfg, store: st, settings: settings}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", a.handleHealth)

	// static.FS is rooted at the asset tree, so css/site.css answers
	// /static/css/site.css.
	mux.Handle("GET /static/", http.StripPrefix("/static/", noDirListing(http.FileServerFS(static.FS))))
	// Browsers ask for /favicon.ico unprompted; an own-origin 404 fails the
	// acceptance walk, so the icon answers at the root as well.
	// The code-block colours come from the render package (Chroma classes);
	// the layout links them as /static/css/code.css.
	codeCSS := []byte(render.CSS())
	mux.HandleFunc("GET /static/css/code.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = w.Write(codeCSS)
	})
	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, static.FS, "images/favicon.ico")
	})

	for _, m := range modules {
		m.Routes(mux)
	}
	return telemetry.Handler(recoverPanic(logRequests(mux)))
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

// noDirListing keeps the static server to files: a directory path (or one
// with a trailing slash) is a 404, never an index listing.
func noDirListing(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "" || strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		if info, err := fs.Stat(static.FS, r.URL.Path); err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}
