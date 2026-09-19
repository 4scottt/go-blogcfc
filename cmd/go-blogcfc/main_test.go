package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestFP_O02_HealthcheckSubcommandExitCodes: the image's HEALTHCHECK is
// this subcommand, so 200 must exit 0 and anything else 1. healthcheck
// returns the code instead of calling os.Exit so the test can assert it.
func TestFP_O02_HealthcheckSubcommandExitCodes(t *testing.T) {
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Errorf("path = %q, want /health", r.URL.Path)
		}
		w.WriteHeader(status)
	}))
	defer srv.Close()

	if code := healthcheck(srv.URL+"/health", 3*time.Second); code != 0 {
		t.Fatalf("exit code on 200 = %d, want 0", code)
	}

	status = http.StatusServiceUnavailable
	if code := healthcheck(srv.URL+"/health", 3*time.Second); code != 1 {
		t.Fatalf("exit code on 503 = %d, want 1", code)
	}

	// Nothing listening at all is also a failed health check.
	srv.Close()
	if code := healthcheck(srv.URL+"/health", time.Second); code != 1 {
		t.Fatalf("exit code with no server = %d, want 1", code)
	}
}

// TestUnknownSubcommandExits2 keeps the CLI's contract explicit.
func TestUnknownSubcommandExits2(t *testing.T) {
	if code := run("nonsense"); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}
