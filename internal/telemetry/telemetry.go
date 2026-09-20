// Package telemetry is the OpenTelemetry wiring: metrics, traces and Go
// runtime instrumentation, all of it optional.
//
// The platform's contract (PLAN §6) is narrow and worth restating, because
// every choice here follows from it:
//
//   - Telemetry off is a no-op. With no OTLP endpoint in the environment no
//     exporter and no provider is built, and the global providers stay the
//     API's noop ones, so nothing is collected and nothing dials out.
//   - With an endpoint set and the collector absent -- normal for the first
//     half-minute after a deploy -- export failures are logged at debug and
//     never fatal. Both of the SDK's diagnostic channels (the error handler
//     and the internal logr logger) are routed into slog at debug level.
//   - The HTTP histogram is the stable convention: http.server.request.duration
//     in seconds with http.response.status_code, which Grafana reads back as
//     http_server_request_duration_seconds_bucket / _count. otelhttp v0.71
//     emits the stable convention only, so OTEL_SEMCONV_STABILITY_OPT_IN is
//     neither set nor consulted.
//   - Its attributes are limited to method, status and route: the extras
//     otelhttp adds (url.scheme, server.address, server.port,
//     network.protocol.*) are dropped by a view, not by an option, because
//     the instrumentation has no option for it.
package telemetry

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/go-logr/logr"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	otelruntime "go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

// defaultServiceName names the resource when the environment does not.
const defaultServiceName = "go-blogcfc"

// endpointVars are the standard variables that turn telemetry on. Any one
// of them is enough: the platform sets the first, the other two are the
// signal-specific overrides the SDK also honours.
var endpointVars = []string{
	"OTEL_EXPORTER_OTLP_ENDPOINT",
	"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
	"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
}

// Enabled reports whether the environment asks for telemetry.
func Enabled() bool {
	for _, v := range endpointVars {
		if strings.TrimSpace(os.Getenv(v)) != "" {
			return true
		}
	}
	return false
}

// Setup installs the metric and trace providers when an OTLP endpoint is
// configured. It returns a shutdown function (never nil, safe to call
// whether or not telemetry is on) and whether telemetry is on.
//
// Everything else is the SDK's own environment handling: the endpoint and
// its headers, OTEL_METRIC_EXPORT_INTERVAL for the periodic reader,
// OTEL_SERVICE_NAME and OTEL_RESOURCE_ATTRIBUTES for the resource.
func Setup(ctx context.Context, logger *slog.Logger) (func(context.Context) error, bool, error) {
	if logger == nil {
		logger = slog.Default()
	}
	// Do this first and unconditionally: from here on nothing the SDK has
	// to say -- a refused connection, a dropped batch -- rises above debug
	// or reaches stderr behind slog's back (O03).
	routeDiagnostics(logger)

	if !Enabled() {
		return func(context.Context) error { return nil }, false, nil
	}

	res, err := newResource()
	if err != nil {
		return func(context.Context) error { return nil }, false, err
	}

	metricExporter, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return func(context.Context) error { return nil }, false, err
	}
	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter)),
		sdkmetric.WithView(views()...),
	)

	traceExporter, err := otlptracehttp.New(ctx)
	if err != nil {
		// The metric side is already running; take it down again so a
		// half-built pipeline never outlives a failed Setup.
		_ = meterProvider.Shutdown(ctx)
		return func(context.Context) error { return nil }, false, err
	}
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(traceExporter),
	)

	otel.SetMeterProvider(meterProvider)
	otel.SetTracerProvider(tracerProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))

	if err := otelruntime.Start(otelruntime.WithMeterProvider(meterProvider)); err != nil {
		logger.Debug("telemetry: runtime metrics unavailable", "error", err)
	}

	// Debug, not info: O03 asks that this package say nothing above debug,
	// and the caller logs the one line that telemetry is on.
	logger.Debug("telemetry on", "endpoint", os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))

	shutdown := func(ctx context.Context) error {
		return errors.Join(tracerProvider.Shutdown(ctx), meterProvider.Shutdown(ctx))
	}
	return shutdown, true, nil
}

// Handler wraps the whole mux in HTTP server instrumentation: one span and
// one duration histogram point per request, named after the route pattern
// the mux matched.
func Handler(next http.Handler) http.Handler { return handler(next) }

// handler is Handler with the options a test needs to install its own
// providers instead of the global ones.
//
// The operation name is empty on purpose: without an explicit span-name
// formatter otelhttp names the span from the stable convention
// ("{method} {route}", falling back to "{method}"), and renames it after
// routing, when http.Request.Pattern holds the matched pattern. That is
// exactly the formatter this package would otherwise have to write, and
// the same Pattern is what the route metric attribute comes from.
func handler(next http.Handler, opts ...otelhttp.Option) http.Handler {
	return otelhttp.NewHandler(next, "", opts...)
}

// views keep the HTTP server instruments to the three attributes the
// platform's dashboards use. otelhttp records more (url.scheme,
// server.address, server.port, network.protocol.name/version) with no
// option to turn them off, and each one multiplies the series Grafana
// stores for no gain: the app is one service behind one proxy.
func views() []sdkmetric.View {
	keep := attribute.NewAllowKeysFilter(
		semconv.HTTPRequestMethodKey,
		semconv.HTTPResponseStatusCodeKey,
		semconv.HTTPRouteKey,
	)
	return []sdkmetric.View{
		sdkmetric.NewView(
			sdkmetric.Instrument{Name: "http.server.*"},
			sdkmetric.Stream{AttributeFilter: keep},
		),
	}
}

// newResource reads OTEL_SERVICE_NAME and OTEL_RESOURCE_ATTRIBUTES through
// the SDK's own detectors, and names the service itself only when the
// environment left it unnamed (the SDK would otherwise call it
// "unknown_service:go-blogcfc").
func newResource() (*resource.Resource, error) {
	if serviceNamedByEnv() {
		return resource.Default(), nil
	}
	return resource.Merge(
		resource.Default(),
		resource.NewSchemaless(semconv.ServiceName(defaultServiceName)),
	)
}

func serviceNamedByEnv() bool {
	if strings.TrimSpace(os.Getenv("OTEL_SERVICE_NAME")) != "" {
		return true
	}
	for _, pair := range strings.Split(os.Getenv("OTEL_RESOURCE_ATTRIBUTES"), ",") {
		if k, _, ok := strings.Cut(pair, "="); ok && strings.TrimSpace(k) == string(semconv.ServiceNameKey) {
			return true
		}
	}
	return false
}

// routeDiagnostics sends both of the SDK's diagnostic channels to slog at
// debug: otel.Handle, which is where a failed export lands, and the
// internal logr logger, which is where everything else does. A missing
// collector is an ordinary state on this platform -- the app starts before
// the collector sidecar -- so it must not colour the log (O03).
func routeDiagnostics(logger *slog.Logger) {
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		logger.Debug("telemetry", "error", err)
	}))
	otel.SetLogger(logr.FromSlogHandler(debugOnly{logger.Handler()}))
}

// debugOnly is a slog.Handler that rewrites every record to debug level.
// The logr bridge maps the SDK's errors to slog errors; this demotes them,
// so LOG_LEVEL=debug is what it takes to see them at all.
type debugOnly struct{ slog.Handler }

func (h debugOnly) Enabled(ctx context.Context, _ slog.Level) bool {
	return h.Handler.Enabled(ctx, slog.LevelDebug)
}

func (h debugOnly) Handle(ctx context.Context, r slog.Record) error {
	r.Level = slog.LevelDebug
	return h.Handler.Handle(ctx, r)
}

func (h debugOnly) WithAttrs(attrs []slog.Attr) slog.Handler {
	return debugOnly{h.Handler.WithAttrs(attrs)}
}

func (h debugOnly) WithGroup(name string) slog.Handler {
	return debugOnly{h.Handler.WithGroup(name)}
}
