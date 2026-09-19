package app

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
)

// statusWriter records the status code and byte count for the log line.
type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// logRequests writes one JSON line per request (PLAN §6: logs are JSON on
// stdout; the collector is the error channel).
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		if sw.status == 0 {
			sw.status = http.StatusOK
		}
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"duration_ms", float64(time.Since(start).Microseconds())/1000.0,
		)
	})
}

// recoverPanic turns a handler panic into a logged 500 instead of a dead
// connection and a dead process.
func recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				slog.Error("panic serving request",
					"method", r.Method, "path", r.URL.Path,
					"panic", v, "stack", string(debug.Stack()))
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("internal server error"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}
