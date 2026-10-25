// Command backend is the registration API for the Dutch Cloud Native Day
// OpenTelemetry workshop (Go edition, service "registration-api-go").
//
// It exposes POST /api/register, which creates three child spans
// (validate-registration, save-registration, send-confirmation-email) under the
// otelhttp-created server span. Failures are classified with a bounded
// error.type, recorded once on the originating child span, exported as a
// correlated ERROR log record, and counted in the registration.duration
// histogram — the three signals the workshop walks through in Dash0.
package main

import (
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const scopeName = "registration-api-go"

// Allowed ticket types (bounded set).
var allowedTicketTypes = map[string]bool{
	"standard": true,
	"workshop": true,
	"vip":      true,
}

// Bounded error.type values (OTel semconv key; low-cardinality, safe on metrics).
const (
	errValidation           = "validation_error"
	errUnknownTicketType    = "unknown_ticket_type"
	errInvalidRecipient     = "invalid_recipient"
	errProviderUnavailable  = "email_provider_unavailable"
)

var (
	tracer      = otel.Tracer(scopeName)
	logger      *slog.Logger            // set in main, backed by the OTel log bridge
	regDuration metric.Float64Histogram // set in main
)

type registerRequest struct {
	Name       string `json:"name"`
	Email      string `json:"email"`
	Company    string `json:"company"`
	TicketType string `json:"ticketType"`
}

// regError is a classified registration failure: a bounded error.type, a
// human-readable message, and the HTTP status that expresses the
// client/business-vs-outage distinction.
type regError struct {
	errorType string
	message   string
	status    int
}

func (e *regError) Error() string { return e.message }

func main() {
	ctx := context.Background()

	shutdown, err := initTelemetry(ctx)
	if err != nil {
		// Do not crash if the exporters cannot be constructed; telemetry is a
		// best-effort concern for the workshop, the app must still serve.
		log.Printf("otel: failed to initialise telemetry: %v (continuing without export)", err)
		shutdown = func(context.Context) error { return nil }
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdown(shutdownCtx)
	}()

	// Created after the providers are set so they are backed by the real SDK
	// (fall back to no-op implementations if init failed — never nil).
	logger = otelslog.NewLogger(scopeName)
	regDuration, err = otel.Meter(scopeName).Float64Histogram(
		"registration.duration",
		metric.WithUnit("s"),
		metric.WithDescription("End-to-end registration handling time"),
	)
	if err != nil {
		log.Printf("otel: failed to create registration.duration histogram: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/register", handleRegister)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// Wrap the whole mux so every request gets a server span with propagated
	// trace context from the frontend.
	handler := otelhttp.NewHandler(mux, "http.server")

	port := os.Getenv("PORT")
	if port == "" {
		port = "3201"
	}
	addr := ":" + port

	srv := &http.Server{Addr: addr, Handler: handler}

	go func() {
		log.Printf("registration-api-go listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

func handleRegister(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	start := time.Now()

	// Metric state, resolved as the handler runs. ticketType stays empty until
	// validation establishes a known-bounded value (so it is omitted from the
	// metric for requests that failed before that point).
	outcome := "confirmed"
	var errType string
	var ticketType string

	defer func() {
		if regDuration == nil {
			return
		}
		attrs := []attribute.KeyValue{attribute.String("outcome", outcome)}
		if ticketType != "" {
			attrs = append(attrs, attribute.String("com.dutchcloudnativeday.registration.ticket_type", ticketType))
		}
		if outcome == "failed" && errType != "" {
			attrs = append(attrs, attribute.String("error.type", errType))
		}
		regDuration.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(attrs...))
	}()

	// fail writes the error response and records the metric/outcome state. The
	// span error + log record were already emitted at the originating span.
	fail := func(re *regError) {
		outcome = "failed"
		errType = re.errorType
		writeJSON(w, re.status, map[string]string{
			"status":    "error",
			"errorType": re.errorType,
			"message":   re.message,
		})
	}

	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(&regError{errValidation, "request body is not valid JSON", http.StatusBadRequest})
		return
	}

	// 1) validate-registration
	if re := validateRegistration(ctx, req); re != nil {
		fail(re)
		return
	}
	ticketType = req.TicketType // now known-bounded

	// 2) save-registration
	saveRegistration(ctx, req)

	// 3) send-confirmation-email
	if re := sendConfirmationEmail(ctx, req); re != nil {
		fail(re)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":         "confirmed",
		"registrationId": newID(),
	})
}

func validateRegistration(ctx context.Context, req registerRequest) *regError {
	ctx, span := tracer.Start(ctx, "validate-registration")
	defer span.End()

	var re *regError
	switch {
	case strings.TrimSpace(req.Name) == "":
		re = &regError{errValidation, "name is required", http.StatusBadRequest}
	case strings.TrimSpace(req.Email) == "":
		re = &regError{errValidation, "email is required", http.StatusBadRequest}
	case !strings.Contains(req.Email, "@"):
		re = &regError{errValidation, "email is malformed (missing @)", http.StatusBadRequest}
	case !allowedTicketTypes[req.TicketType]:
		re = &regError{errUnknownTicketType,
			fmt.Sprintf("unknown ticket type %q (allowed: standard, workshop, vip)", req.TicketType),
			http.StatusBadRequest}
	}

	if re != nil {
		recordFailure(ctx, span, re)
		return re
	}

	// Only now that the ticket type is known-bounded do we put it on the span,
	// along with the low-cardinality email domain (never the full address: PII
	// and unbounded).
	span.SetAttributes(
		attribute.String("com.dutchcloudnativeday.registration.ticket_type", req.TicketType),
		attribute.String("com.dutchcloudnativeday.registration.email_domain", emailDomain(req.Email)),
	)
	return nil
}

func saveRegistration(ctx context.Context, req registerRequest) {
	_, span := tracer.Start(ctx, "save-registration")
	defer span.End()

	span.SetAttributes(attribute.String("db.system.name", "postgresql"))

	// 10% of writes are "slow queries" (400-700ms), the latency outlier the
	// workshop investigates; otherwise a normal 20-120ms write. The slow case is
	// visible as the span's own duration (and in the registration.duration
	// metric) — no extra ad-hoc attribute needed.
	var d time.Duration
	if rand.Float64() < 0.10 {
		d = time.Duration(400+rand.IntN(301)) * time.Millisecond
	} else {
		d = time.Duration(20+rand.IntN(101)) * time.Millisecond
	}
	time.Sleep(d)
}

func sendConfirmationEmail(ctx context.Context, req registerRequest) *regError {
	ctx, span := tracer.Start(ctx, "send-confirmation-email")
	defer span.End()

	// Simulate calling an email provider: 15-60ms.
	time.Sleep(time.Duration(15+rand.IntN(46)) * time.Millisecond)

	var re *regError
	switch {
	case strings.Contains(req.Email, "+fail"):
		// Bad recipient — the client's problem (bounced/invalid address).
		re = &regError{errInvalidRecipient,
			"email address was rejected as an invalid recipient (+fail)",
			http.StatusUnprocessableEntity}
	case rand.Float64() < 0.15:
		// Transient server-side outage of the email provider.
		re = &regError{errProviderUnavailable,
			"email provider is temporarily unavailable",
			http.StatusServiceUnavailable}
	}

	if re != nil {
		recordFailure(ctx, span, re)
		return re
	}
	return nil
}

// recordFailure records the failure exactly once on the originating child span
// (exception + ERROR status + bounded error.type) and emits a correlated ERROR
// log record (within the span's context, so Dash0 links the log to the span).
// It deliberately does not touch the parent/server span — that would
// double-count the error; the server span's status follows from the HTTP status
// as otelhttp already handles.
func recordFailure(ctx context.Context, span trace.Span, re *regError) {
	err := errors.New(re.message)
	span.SetAttributes(attribute.String("error.type", re.errorType))
	span.RecordError(err)
	span.SetStatus(codes.Error, re.message)
	logger.ErrorContext(ctx, re.message, slog.String("error.type", re.errorType))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func emailDomain(email string) string {
	if i := strings.LastIndex(email, "@"); i >= 0 && i < len(email)-1 {
		return email[i+1:]
	}
	return "unknown"
}

// newID returns a random hex id. Falls back to a timestamp-based value only if
// the system RNG fails (effectively never).
func newID() string {
	b := make([]byte, 16)
	if _, err := crand.Read(b); err != nil {
		return fmt.Sprintf("reg-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
