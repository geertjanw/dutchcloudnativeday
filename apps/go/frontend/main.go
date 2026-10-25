// Command frontend is the registration web UI for the Dutch Cloud Native Day
// OpenTelemetry workshop (Go edition, service "registration-web-go").
//
// It serves the registration form at GET /, and on POST /register forwards the
// submission to the backend's POST /api/register using an otelhttp-instrumented
// client so the W3C traceparent header propagates and the whole registration
// appears as one distributed trace spanning both services in Dash0.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"html"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// httpClient's transport is instrumented so outbound requests carry the
// traceparent header derived from the incoming request context.
var httpClient = &http.Client{
	Timeout:   10 * time.Second,
	Transport: otelhttp.NewTransport(http.DefaultTransport),
}

func backendURL() string {
	if v := os.Getenv("BACKEND_URL"); v != "" {
		return v
	}
	return "http://localhost:3201"
}

type backendResponse struct {
	Status         string `json:"status"`
	RegistrationID string `json:"registrationId"`
	ErrorType      string `json:"errorType"`
	Message        string `json:"message"`
}

func main() {
	ctx := context.Background()

	shutdown, err := initTracer(ctx)
	if err != nil {
		log.Printf("otel: failed to initialise tracer: %v (continuing without export)", err)
		shutdown = func(context.Context) error { return nil }
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdown(shutdownCtx)
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", handleIndex)
	mux.HandleFunc("POST /register", handleRegister)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	handler := otelhttp.NewHandler(mux, "http.server")

	port := os.Getenv("PORT")
	if port == "" {
		port = "3200"
	}
	addr := ":" + port

	srv := &http.Server{Addr: addr, Handler: handler}

	go func() {
		log.Printf("registration-web-go listening on %s (backend=%s)", addr, backendURL())
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

func handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, indexHTML)
}

func handleRegister(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := r.ParseForm(); err != nil {
		renderError(w, http.StatusBadRequest, "could not parse form")
		return
	}

	name := r.FormValue("name")
	email := r.FormValue("email")
	company := r.FormValue("company")
	ticketType := r.FormValue("ticketType")

	payload, _ := json.Marshal(map[string]string{
		"name":       name,
		"email":      email,
		"company":    company,
		"ticketType": ticketType,
	})

	// Use r.Context() so the otelhttp transport injects traceparent from the
	// current server span into the outbound request to the backend.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, backendURL()+"/api/register", bytes.NewReader(payload))
	if err != nil {
		renderError(w, http.StatusInternalServerError, "could not build backend request")
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		// The backend is actually unreachable — this is the only case that maps
		// to 502; classified backend failures keep their own status below.
		renderError(w, http.StatusBadGateway, "backend unreachable: "+err.Error())
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var br backendResponse
	_ = json.Unmarshal(body, &br)

	if resp.StatusCode == http.StatusOK && br.Status == "confirmed" {
		renderSuccess(w, name, email, br.RegistrationID)
		return
	}

	msg := br.Message
	if msg == "" {
		msg = "registration failed"
	}
	if br.ErrorType != "" {
		msg = msg + " (" + br.ErrorType + ")"
	}
	// Mirror the backend's status class (400/422/503) so the client/outage
	// distinction stays visible instead of flattening to one code.
	status := resp.StatusCode
	if status < 400 {
		status = http.StatusBadGateway
	}
	renderError(w, status, msg)
}

func renderSuccess(w http.ResponseWriter, name, email, registrationID string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	page := successHTML
	page = strings.ReplaceAll(page, "{{name}}", html.EscapeString(name))
	page = strings.ReplaceAll(page, "{{email}}", html.EscapeString(email))
	page = strings.ReplaceAll(page, "{{registrationId}}", html.EscapeString(registrationID))
	_, _ = io.WriteString(w, page)
}

func renderError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	page := strings.ReplaceAll(errorHTML, "{{error}}", html.EscapeString(msg))
	_, _ = io.WriteString(w, page)
}
