package main

import (
	"context"
	"log"
	"net/url"
	"os"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otellog "go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// shutdownFunc flushes and stops an exporter/provider.
type shutdownFunc func(context.Context) error

// initTelemetry wires up traces, metrics, and logs, all exporting over OTLP/HTTP
// to the same Dash0 (or local) endpoint with the same auth headers. It returns a
// single shutdown function that flushes all three providers.
//
// Export is best-effort: construction does not dial the collector, and the batch
// processors / periodic reader log (but do not crash on) export errors, so the
// app keeps serving even when the collector is unreachable.
//
// Env used:
//   - OTEL_EXPORTER_OTLP_ENDPOINT  base ingress URL, e.g. https://ingress.eu-west-1.aws.dash0.com
//   - DASH0_AUTH_TOKEN             the auth_... token (sent as "Authorization: Bearer ...")
//   - DASH0_DATASET                target dataset (sent as "Dash0-Dataset")
//   - OTEL_SERVICE_NAME            resource service.name
func initTelemetry(ctx context.Context) (shutdownFunc, error) {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://localhost:4318"
	}
	host, insecure := parseEndpoint(endpoint)

	res, err := buildResource()
	if err != nil {
		return nil, err
	}

	var shutdowns []shutdownFunc
	shutdownAll := func(ctx context.Context) error {
		var firstErr error
		for i := len(shutdowns) - 1; i >= 0; i-- {
			if err := shutdowns[i](ctx); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		return firstErr
	}

	// --- Traces ---
	traceExp, err := otlptracehttp.New(ctx, traceOpts(host, insecure)...)
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	shutdowns = append(shutdowns, tp.Shutdown)

	// --- Metrics ---
	metricExp, err := otlpmetrichttp.New(ctx, metricOpts(host, insecure)...)
	if err != nil {
		return nil, err
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp)),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)
	shutdowns = append(shutdowns, mp.Shutdown)

	// --- Logs ---
	logExp, err := otlploghttp.New(ctx, logOpts(host, insecure)...)
	if err != nil {
		return nil, err
	}
	lp := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp)),
		sdklog.WithResource(res),
	)
	otellog.SetLoggerProvider(lp)
	shutdowns = append(shutdowns, lp.Shutdown)

	log.Printf("otel: exporting traces, metrics and logs for %q to %s (tls=%t)",
		serviceName(), host, !insecure)

	return shutdownAll, nil
}

func serviceName() string {
	if v := os.Getenv("OTEL_SERVICE_NAME"); v != "" {
		return v
	}
	return "registration-api-go"
}

func buildResource() (*resource.Resource, error) {
	return resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(serviceName()),
		),
	)
}

func traceOpts(host string, insecure bool) []otlptracehttp.Option {
	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(host),
		otlptracehttp.WithHeaders(dash0Headers()),
	}
	if insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}
	return opts
}

func metricOpts(host string, insecure bool) []otlpmetrichttp.Option {
	opts := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpoint(host),
		otlpmetrichttp.WithHeaders(dash0Headers()),
	}
	if insecure {
		opts = append(opts, otlpmetrichttp.WithInsecure())
	}
	return opts
}

func logOpts(host string, insecure bool) []otlploghttp.Option {
	opts := []otlploghttp.Option{
		otlploghttp.WithEndpoint(host),
		otlploghttp.WithHeaders(dash0Headers()),
	}
	if insecure {
		opts = append(opts, otlploghttp.WithInsecure())
	}
	return opts
}

// dash0Headers are the auth headers sent with every OTLP request, built in code
// from the environment (robust across SDKs — see SPEC.md).
func dash0Headers() map[string]string {
	return map[string]string{
		"Authorization": "Bearer " + os.Getenv("DASH0_AUTH_TOKEN"),
		"Dash0-Dataset": os.Getenv("DASH0_DATASET"),
	}
}

// parseEndpoint strips the scheme off an OTLP endpoint and returns the
// host[:port] plus whether the connection should be insecure (plain http).
// It accepts bare host:port, http:// and https:// forms.
func parseEndpoint(endpoint string) (host string, insecure bool) {
	endpoint = strings.TrimSpace(endpoint)
	switch {
	case strings.HasPrefix(endpoint, "http://"):
		insecure = true
	case strings.HasPrefix(endpoint, "https://"):
		insecure = false
	default:
		// No scheme: treat as host:port. Default to secure.
		return strings.TrimSuffix(endpoint, "/"), false
	}

	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		// Fall back to a naive strip.
		h := strings.TrimPrefix(strings.TrimPrefix(endpoint, "http://"), "https://")
		return strings.TrimSuffix(h, "/"), insecure
	}
	return u.Host, insecure
}
