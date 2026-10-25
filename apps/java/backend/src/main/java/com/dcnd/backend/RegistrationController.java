package com.dcnd.backend;

import io.opentelemetry.api.GlobalOpenTelemetry;
import io.opentelemetry.api.common.Attributes;
import io.opentelemetry.api.common.AttributesBuilder;
import io.opentelemetry.api.metrics.DoubleHistogram;
import io.opentelemetry.api.trace.Span;
import io.opentelemetry.api.trace.StatusCode;
import io.opentelemetry.api.trace.Tracer;
import io.opentelemetry.context.Scope;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.http.MediaType;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RestController;

import java.util.LinkedHashMap;
import java.util.Map;
import java.util.Set;
import java.util.UUID;
import java.util.concurrent.ThreadLocalRandom;

/**
 * Backend registration handler.
 *
 * The three logical steps (validate / save / send-email) each run inside a
 * manual OpenTelemetry child span. The javaagent creates the parent server
 * span automatically and wires GlobalOpenTelemetry, so the tracer/meter
 * obtained here produce children of the incoming request span and export
 * through the agent's SDK.
 */
@RestController
public class RegistrationController {

    private static final Logger log = LoggerFactory.getLogger(RegistrationController.class);

    private static final Set<String> ALLOWED_TICKET_TYPES = Set.of("standard", "workshop", "vip");

    private final Tracer tracer = GlobalOpenTelemetry.getTracer("registration");

    // One histogram, recorded once per /api/register call. The javaagent wires
    // the MeterProvider and exports metrics over OTLP.
    private final DoubleHistogram registrationDuration = GlobalOpenTelemetry.getMeter("registration")
            .histogramBuilder("registration.duration")
            .setUnit("s")
            .setDescription("End-to-end registration handling time")
            .build();

    @GetMapping("/healthz")
    public ResponseEntity<String> healthz() {
        return ResponseEntity.ok("ok");
    }

    @PostMapping(value = "/api/register",
            consumes = MediaType.APPLICATION_JSON_VALUE,
            produces = MediaType.APPLICATION_JSON_VALUE)
    public ResponseEntity<Map<String, Object>> register(@RequestBody(required = false) Registration req) {
        Registration r = req == null ? new Registration() : req;
        long startNanos = System.nanoTime();

        // Null until validation establishes a bounded, known-good ticket type.
        String validatedTicketType = null;

        try {
            validatedTicketType = validateRegistration(r);
            saveRegistration(r);
            sendConfirmationEmail(r);

            recordDuration(startNanos, validatedTicketType, "confirmed", null);

            String registrationId = UUID.randomUUID().toString();
            Map<String, Object> ok = new LinkedHashMap<>();
            ok.put("status", "confirmed");
            ok.put("registrationId", registrationId);
            return ResponseEntity.ok(ok);
        } catch (RegistrationException e) {
            recordDuration(startNanos, validatedTicketType, "failed", e.errorType);

            // Emit the failure as an OTel log record too (the javaagent's Logback
            // appender exports it over OTLP, correlated to the current trace).
            log.error(e.getMessage(), e);

            Map<String, Object> err = new LinkedHashMap<>();
            err.put("status", "error");
            err.put("errorType", e.errorType);
            err.put("message", e.getMessage());
            return ResponseEntity.status(e.httpStatus).body(err);
        }
    }

    // 1. validate-registration — returns the validated ticket type on success.
    private String validateRegistration(Registration r) {
        Span span = tracer.spanBuilder("validate-registration").startSpan();
        try (Scope scope = span.makeCurrent()) {
            String email = r.email == null ? "" : r.email;
            String ticketType = r.ticketType == null ? "" : r.ticketType;

            if (r.name == null || r.name.isBlank()) {
                throw fail(span, "validation_error", 400, "name is required");
            }
            if (email.isBlank()) {
                throw fail(span, "validation_error", 400, "email is required");
            }
            if (!email.contains("@")) {
                throw fail(span, "validation_error", 400, "email must contain @");
            }
            if (!ALLOWED_TICKET_TYPES.contains(ticketType)) {
                throw fail(span, "unknown_ticket_type", 400,
                        "unknown ticket type: " + ticketType);
            }

            // Only now, after it passed, record the bounded value + domain (no PII).
            span.setAttribute("com.dutchcloudnativeday.registration.ticket_type", ticketType);
            span.setAttribute("com.dutchcloudnativeday.registration.email_domain", emailDomain(email));
            return ticketType;
        } finally {
            span.end();
        }
    }

    // 2. save-registration
    private void saveRegistration(Registration r) {
        Span span = tracer.spanBuilder("save-registration").startSpan();
        try (Scope scope = span.makeCurrent()) {
            span.setAttribute("db.system.name", "postgresql");
            long ms;
            boolean slow = ThreadLocalRandom.current().nextDouble() < 0.10;
            if (slow) {
                ms = ThreadLocalRandom.current().nextLong(400, 701); // slow query outlier
            } else {
                ms = ThreadLocalRandom.current().nextLong(20, 121);
            }
            // The slow case shows up as this span's own duration (and in the
            // registration.duration metric) — no ad-hoc attribute needed.
            sleep(ms);
        } finally {
            span.end();
        }
    }

    // 3. send-confirmation-email
    private void sendConfirmationEmail(Registration r) {
        Span span = tracer.spanBuilder("send-confirmation-email").startSpan();
        try (Scope scope = span.makeCurrent()) {
            String email = r.email == null ? "" : r.email;
            sleep(ThreadLocalRandom.current().nextLong(15, 61));

            if (email.contains("+fail")) {
                // Bad recipient — the client's problem.
                throw fail(span, "invalid_recipient", 422,
                        "email provider rejected recipient address");
            }
            if (ThreadLocalRandom.current().nextDouble() < 0.15) {
                // Transient provider outage — server-side.
                throw fail(span, "email_provider_unavailable", 503,
                        "email provider temporarily unavailable");
            }
        } finally {
            span.end();
        }
    }

    /**
     * Record the exception + ERROR status + bounded {@code error.type} on the
     * single child span where the failure originates, then return the exception
     * to throw. The server span is intentionally left untouched to avoid
     * double-counting the error.
     */
    private RegistrationException fail(Span span, String errorType, int httpStatus, String message) {
        RegistrationException e = new RegistrationException(errorType, httpStatus, message);
        span.setAttribute("error.type", errorType);
        span.setStatus(StatusCode.ERROR, message);
        span.recordException(e);
        return e;
    }

    private void recordDuration(long startNanos, String ticketType, String outcome, String errorType) {
        double seconds = (System.nanoTime() - startNanos) / 1_000_000_000.0;
        AttributesBuilder attrs = Attributes.builder().put("outcome", outcome);
        if (ticketType != null) {
            attrs.put("com.dutchcloudnativeday.registration.ticket_type", ticketType);
        }
        if (errorType != null) {
            attrs.put("error.type", errorType);
        }
        registrationDuration.record(seconds, attrs.build());
    }

    private static String emailDomain(String email) {
        int at = email.lastIndexOf('@');
        return at >= 0 && at < email.length() - 1 ? email.substring(at + 1) : "unknown";
    }

    private static void sleep(long ms) {
        try {
            Thread.sleep(ms);
        } catch (InterruptedException ie) {
            Thread.currentThread().interrupt();
        }
    }

    /** Request body for POST /api/register. */
    public static class Registration {
        public String name;
        public String email;
        public String company;
        public String ticketType;
    }

    /** Carries the bounded error.type and the HTTP status to return. */
    private static class RegistrationException extends RuntimeException {
        final String errorType;
        final int httpStatus;

        RegistrationException(String errorType, int httpStatus, String message) {
            super(message);
            this.errorType = errorType;
            this.httpStatus = httpStatus;
        }
    }
}
