package main

import (
	"context"
	"log"
	"net/url"
	"os"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// initTracer configures an OTLP/HTTP trace exporter pointed at Dash0 (or any
// OTLP endpoint) using values read from the environment. It returns a shutdown
// function that flushes and stops the batch processor.
//
// Env used:
//   - OTEL_EXPORTER_OTLP_ENDPOINT  base ingress URL, e.g. https://ingress.eu-west-1.aws.dash0.com
//   - DASH0_AUTH_TOKEN             the auth_... token (sent as "Authorization: Bearer ...")
//   - DASH0_DATASET                target dataset (sent as "Dash0-Dataset")
//   - OTEL_SERVICE_NAME            resource service.name
func initTracer(ctx context.Context) (func(context.Context) error, error) {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://localhost:4318"
	}

	host, insecure := parseEndpoint(endpoint)

	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(host),
		otlptracehttp.WithHeaders(map[string]string{
			"Authorization": "Bearer " + os.Getenv("DASH0_AUTH_TOKEN"),
			"Dash0-Dataset": os.Getenv("DASH0_DATASET"),
		}),
	}
	// Only disable TLS for plain-http endpoints (e.g. a local collector on 4318).
	// For Dash0 (https) we keep TLS on by NOT setting WithInsecure.
	if insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}

	exporter, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, err
	}

	serviceName := os.Getenv("OTEL_SERVICE_NAME")
	if serviceName == "" {
		serviceName = "registration-service-go"
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(serviceName),
		),
	)
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	log.Printf("otel: exporting traces for %q to %s (tls=%t)", serviceName, host, !insecure)

	return tp.Shutdown, nil
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
		// No scheme: treat as host:port. Default to secure unless clearly local.
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
