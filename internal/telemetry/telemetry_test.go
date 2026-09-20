package telemetry

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// capture is a slog.Handler that keeps every record, so a test can ask
// what level the telemetry stack logged at.
type capture struct {
	mu      sync.Mutex
	records []slog.Record
}

func (c *capture) Enabled(context.Context, slog.Level) bool { return true }

func (c *capture) Handle(_ context.Context, r slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, r.Clone())
	return nil
}

func (c *capture) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *capture) WithGroup(string) slog.Handler      { return c }

// above returns the records logged at more than the given level.
func (c *capture) above(level slog.Level) []slog.Record {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []slog.Record
	for _, r := range c.records {
		if r.Level > level {
			out = append(out, r)
		}
	}
	return out
}

func (c *capture) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.records)
}

// closedPort returns an address on the loopback that nothing listens on,
// which is what the collector's address looks like for the first half
// minute after a deploy.
func closedPort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return addr
}

// testMux is the shape the app has: patterns with wildcards, so the route
// attribute is the pattern and never the path.
const testPattern = "GET /2026/{month}"

func testMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc(testPattern, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}

// TestFP_O03_NoOpWithoutEnvAndNoErrorAboveDebugWithCollectorAbsent covers
// both halves of the platform's promise: nothing is built when no endpoint
// is configured, and a configured endpoint with nothing listening on it is
// a debug-level matter, never an error and never fatal (PLAN §6, FP O03).
func TestFP_O03_NoOpWithoutEnvAndNoErrorAboveDebugWithCollectorAbsent(t *testing.T) {
	// This half must run before anything installs a provider globally.
	t.Run("off", func(t *testing.T) {
		for _, v := range endpointVars {
			t.Setenv(v, "")
		}
		// The globals are process-wide, so what this asserts is that Setup
		// changes nothing: on a fresh process they are the API's own
		// noop-backed delegates, and they are still whatever they were.
		beforeMeter, beforeTracer := otel.GetMeterProvider(), otel.GetTracerProvider()
		_, meterWasSDK := beforeMeter.(*sdkmetric.MeterProvider)
		_, tracerWasSDK := beforeTracer.(*sdktrace.TracerProvider)

		buf := &capture{}
		shutdown, enabled, err := Setup(context.Background(), slog.New(buf))
		if err != nil {
			t.Fatalf("Setup: %v", err)
		}
		if enabled {
			t.Fatal("enabled = true with no endpoint in the environment")
		}
		if shutdown == nil {
			t.Fatal("shutdown is nil; it must be safe to call unconditionally")
		}
		if err := shutdown(context.Background()); err != nil {
			t.Fatalf("no-op shutdown: %v", err)
		}
		// No provider was constructed: nothing records and nothing dials.
		if otel.GetMeterProvider() != beforeMeter {
			t.Error("Setup installed a meter provider with telemetry off")
		}
		if otel.GetTracerProvider() != beforeTracer {
			t.Error("Setup installed a tracer provider with telemetry off")
		}
		if !meterWasSDK {
			if _, ok := otel.GetMeterProvider().(*sdkmetric.MeterProvider); ok {
				t.Error("the global meter provider is an SDK provider; telemetry off must build none")
			}
		}
		if !tracerWasSDK {
			if _, ok := otel.GetTracerProvider().(*sdktrace.TracerProvider); ok {
				t.Error("the global tracer provider is an SDK provider; telemetry off must build none")
			}
		}
		if n := buf.count(); n != 0 {
			t.Errorf("Setup logged %d records with telemetry off, want none", n)
		}
	})

	t.Run("collector absent", func(t *testing.T) {
		t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://"+closedPort(t))
		// Both intervals are a count of milliseconds, not a duration: the
		// reader exports five times a second and each attempt gives up
		// after 200ms, so the failures land inside the test's patience.
		t.Setenv("OTEL_METRIC_EXPORT_INTERVAL", "200")
		t.Setenv("OTEL_EXPORTER_OTLP_TIMEOUT", "200")
		t.Setenv("OTEL_SERVICE_NAME", "go-blogcfc-test")

		buf := &capture{}
		shutdown, enabled, err := Setup(context.Background(), slog.New(buf))
		if err != nil {
			t.Fatalf("Setup: %v", err)
		}
		if !enabled {
			t.Fatal("enabled = false with OTEL_EXPORTER_OTLP_ENDPOINT set")
		}

		h := Handler(testMux())
		for i := 0; i < 3; i++ {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/2026/0"+strconv.Itoa(i+1), nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("request %d = %d, want 200", i, rec.Code)
			}
		}

		// Long enough for several export attempts against the closed port.
		time.Sleep(1500 * time.Millisecond)

		if loud := buf.above(slog.LevelDebug); len(loud) > 0 {
			for _, r := range loud {
				t.Errorf("logged above debug with the collector absent: [%s] %s", r.Level, r.Message)
			}
		}
		if buf.count() == 0 {
			t.Error("no debug record at all: the SDK's failures are not reaching the logger")
		}

		// Shutdown with the collector still absent: it reports the failed
		// final export (a wrapped connection-refused error from each
		// provider, joined) and must not panic or hang.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		err = shutdown(ctx)
		t.Logf("shutdown returned: %v", err)
		if loud := buf.above(slog.LevelDebug); len(loud) > 0 {
			t.Errorf("shutdown logged above debug: [%s] %s", loud[0].Level, loud[0].Message)
		}
	})
}

// TestFP_O04_HTTPMetricsStableSemconv reads the histogram back from an
// in-memory reader: the stable convention's name and unit, and only the
// three attributes the platform's dashboards group by (PLAN §6, FP O04).
func TestFP_O04_HTTPMetricsStableSemconv(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader), sdkmetric.WithView(views()...))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	spans := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	h := handler(testMux(), otelhttp.WithMeterProvider(mp), otelhttp.WithTracerProvider(tp))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/2026/09", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /2026/09 = %d, want 200", rec.Code)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}

	var found *metricdata.Metrics
	var names []string
	for _, sm := range rm.ScopeMetrics {
		for i, m := range sm.Metrics {
			names = append(names, m.Name)
			if m.Name == "http.server.request.duration" {
				found = &sm.Metrics[i]
			}
		}
	}
	if found == nil {
		t.Fatalf("no http.server.request.duration; collected %v", names)
	}
	if found.Unit != "s" {
		t.Errorf("unit = %q, want %q (seconds, the stable convention)", found.Unit, "s")
	}

	hist, ok := found.Data.(metricdata.Histogram[float64])
	if !ok {
		t.Fatalf("http.server.request.duration is %T, want a float64 histogram", found.Data)
	}
	if len(hist.DataPoints) != 1 {
		t.Fatalf("%d data points, want 1", len(hist.DataPoints))
	}
	dp := hist.DataPoints[0]
	if dp.Count != 1 {
		t.Errorf("count = %d, want 1", dp.Count)
	}

	want := attribute.NewSet(
		attribute.String("http.request.method", http.MethodGet),
		attribute.Int("http.response.status_code", http.StatusOK),
		attribute.String("http.route", "/2026/{month}"),
	)
	if !dp.Attributes.Equals(&want) {
		t.Errorf("attributes = %v, want exactly %v", dp.Attributes.Encoded(attribute.DefaultEncoder()),
			want.Encoded(attribute.DefaultEncoder()))
	}

	// The span is named after the same matched pattern, not the path.
	ended := spans.Ended()
	if len(ended) != 1 {
		t.Fatalf("%d spans, want 1", len(ended))
	}
	if got := ended[0].Name(); got != testPattern {
		t.Errorf("span name = %q, want %q", got, testPattern)
	}
	for _, a := range ended[0].Attributes() {
		if strings.HasPrefix(string(a.Key), "net.") {
			t.Errorf("span carries an old-convention attribute %s", a.Key)
		}
	}
}
