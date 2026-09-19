// Command go-blogcfc serves the blog and owns its schema.
//
//	go-blogcfc serve        # migrate, seed, then listen
//	go-blogcfc migrate      # migrate and seed, then exit
//	go-blogcfc seed-admin   # seed the admin user from ADMIN_PASSWORD
//	go-blogcfc healthcheck  # GET /health on 127.0.0.1, for the image's HEALTHCHECK
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	// The time zone database is embedded so a `timezone` setting works on
	// a distroless image without relying on the base's tzdata.
	_ "time/tzdata"

	_ "github.com/go-sql-driver/mysql"

	"github.com/4scottt/go-blogcfc/internal/app"
	"github.com/4scottt/go-blogcfc/internal/config"
	"github.com/4scottt/go-blogcfc/internal/migrate"
	"github.com/4scottt/go-blogcfc/internal/store"
)

const usage = `usage: go-blogcfc <serve|migrate|seed-admin|healthcheck>`

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel()})))

	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	os.Exit(run(cmd))
}

func run(cmd string) int {
	switch cmd {
	case "serve":
		if err := serve(); err != nil {
			slog.Error("serve failed", "error", err)
			return 1
		}
		return 0
	case "migrate":
		if err := runMigrate(); err != nil {
			slog.Error("migrate failed", "error", err)
			return 1
		}
		return 0
	case "seed-admin":
		if err := runSeedAdmin(); err != nil {
			slog.Error("seed-admin failed", "error", err)
			return 1
		}
		return 0
	case "healthcheck":
		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return healthcheck(fmt.Sprintf("http://127.0.0.1:%d/health", cfg.Port), 3*time.Second)
	case "-h", "--help", "help":
		fmt.Println(usage)
		return 0
	default:
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}
}

// logLevel honours LOG_LEVEL=debug for a noisier run; the default is info.
func logLevel() slog.Level {
	if os.Getenv("LOG_LEVEL") == "debug" {
		return slog.LevelDebug
	}
	return slog.LevelInfo
}

// openAndMigrate is the shared prelude: config, pool, schema, seeds.
func openAndMigrate(ctx context.Context) (*config.Config, *store.Store, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, err
	}
	st, err := store.Open(cfg.DSN())
	if err != nil {
		return nil, nil, err
	}
	if err := waitForDB(ctx, st, 30*time.Second); err != nil {
		_ = st.Close()
		return nil, nil, err
	}
	if err := migrate.Up(ctx, st.DB()); err != nil {
		_ = st.Close()
		return nil, nil, err
	}
	if err := migrate.Seed(ctx, st.DB()); err != nil {
		_ = st.Close()
		return nil, nil, err
	}
	return cfg, st, nil
}

// waitForDB gives a sidecar database a moment to accept connections after
// a deploy recreates both containers at once.
func waitForDB(ctx context.Context, st *store.Store, limit time.Duration) error {
	deadline := time.Now().Add(limit)
	for {
		pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err := st.Ping(pingCtx)
		cancel()
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("database unreachable after %s: %w", limit, err)
		}
		slog.Info("waiting for the database", "error", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func runMigrate() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cfg, st, err := openAndMigrate(ctx)
	if err != nil {
		return err
	}
	defer st.Close()
	slog.Info("schema up to date", "database", cfg.DBName)
	return nil
}

func runSeedAdmin() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cfg, st, err := openAndMigrate(ctx)
	if err != nil {
		return err
	}
	defer st.Close()
	if cfg.AdminPassword == "" {
		return errors.New("ADMIN_PASSWORD is not set")
	}
	return migrate.SeedAdmin(ctx, st, cfg.AdminPassword)
}

func serve() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, st, err := openAndMigrate(ctx)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := cfg.ValidateForServe(); err != nil {
		return err
	}

	// ADMIN_PASSWORD seeds the first user and is ignored after that.
	if cfg.AdminPassword != "" {
		if err := migrate.SeedAdmin(ctx, st, cfg.AdminPassword); err != nil {
			return err
		}
	}

	settings := config.NewSettings(st, cfg)
	if err := settings.Reload(ctx); err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           app.New(cfg, st, settings),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", srv.Addr, "base_url", cfg.BlogBaseURL, "data_dir", cfg.DataDir)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		return nil
	}
}

// healthcheck GETs the health endpoint and returns the process exit code:
// 0 when it answers 200, 1 otherwise. It returns rather than exits so a
// test can assert the codes (FP O02).
func healthcheck(url string, timeout time.Duration) int {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "health: %s\n", resp.Status)
		return 1
	}
	return 0
}
