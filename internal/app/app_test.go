package app_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/4scottt/go-blogcfc/internal/app"
	"github.com/4scottt/go-blogcfc/internal/config"
	"github.com/4scottt/go-blogcfc/internal/store"
	"github.com/4scottt/go-blogcfc/internal/testdb"
)

func testConfig() *config.Config {
	return &config.Config{Port: 8080, BlogBaseURL: "http://localhost:8081", DataDir: "/tmp"}
}

func handlerFor(t *testing.T, st *store.Store) http.Handler {
	t.Helper()
	cfg := testConfig()
	settings := config.NewSettings(st, cfg)
	_ = settings.Reload(context.Background()) // an unreachable database leaves the cache empty
	return app.New(cfg, st, settings)
}

// TestFP_O02_HealthOkWithoutAuth: the platform probe answers 200 `ok` with
// no session and no other work.
func TestFP_O02_HealthOkWithoutAuth(t *testing.T) {
	st := testdb.New(t)
	h := handlerFor(t, st)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "ok" {
		t.Fatalf("body = %q, want %q", got, "ok")
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Fatalf("content-type = %q, want text/plain", got)
	}
}

// TestFP_O02_HealthReportsDbDown: an unreachable database is a 503, not a
// hang and not a 200.
func TestFP_O02_HealthReportsDbDown(t *testing.T) {
	// Port 1 is never listening; sql.Open does not dial, so this is the
	// same shape as a database that has gone away.
	st, err := store.Open("goblogcfc:goblogcfc@tcp(127.0.0.1:1)/goblogcfc?parseTime=true&loc=UTC")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	h := handlerFor(t, st)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "db unavailable" {
		t.Fatalf("body = %q, want %q", got, "db unavailable")
	}
}

// TestStaticAssetsServed: /static/ serves the embedded tree, and a
// missing asset is a 404 rather than a panic.
func TestStaticAssetsServed(t *testing.T) {
	st := testdb.New(t)
	h := handlerFor(t, st)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/css/site.css", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/css/nothing.css", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing asset status = %d, want 404", rec.Code)
	}
}

// TestModulesRegisterRoutes: a later package's routes land on the
// same mux and run through the middleware.
func TestModulesRegisterRoutes(t *testing.T) {
	st := testdb.New(t)
	cfg := testConfig()
	settings := config.NewSettings(st, cfg)
	if err := settings.Reload(context.Background()); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	h := app.New(cfg, st, settings, moduleFunc(func(mux *http.ServeMux) {
		mux.HandleFunc("GET /module", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("from the module"))
		})
		mux.HandleFunc("GET /boom", func(http.ResponseWriter, *http.Request) { panic("boom") })
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/module", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "from the module" {
		t.Fatalf("module route: %d %q", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("panicking route status = %d, want 500", rec.Code)
	}
}

type moduleFunc func(mux *http.ServeMux)

func (f moduleFunc) Routes(mux *http.ServeMux) { f(mux) }
