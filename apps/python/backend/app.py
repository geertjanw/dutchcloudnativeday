# OpenTelemetry must be set up BEFORE the Flask app is created.
import otel_setup  # noqa: F401  (import for side effects: providers + instrumentation)
from otel_setup import tracer, meter, logger, instrument_app

import os
import random
import time
import uuid

from flask import Flask, jsonify, request
from opentelemetry.trace import Status, StatusCode

app = Flask(__name__)
instrument_app(app)

ALLOWED_TICKET_TYPES = {"standard", "workshop", "vip"}

# One histogram measurement per /api/register call. Bounded attributes only.
registration_duration = meter.create_histogram(
    name="registration.duration",
    unit="s",
    description="End-to-end registration handling time",
)


class RegistrationError(Exception):
    """A classified registration failure.

    Carries a bounded ``error_type`` (OTel semconv ``error.type``) and the HTTP
    status the API should respond with.
    """

    def __init__(self, error_type, status_code, message):
        super().__init__(message)
        self.error_type = error_type
        self.status_code = status_code
        self.message = message


def _fail_on_current_span(exc):
    """Record the exception + ERROR status + error.type on the ONE span where
    it occurred (the current span). Must be called inside that span's context."""
    span = otel_setup.trace.get_current_span()
    span.set_attribute("error.type", exc.error_type)
    span.record_exception(exc)
    span.set_status(Status(StatusCode.ERROR, exc.message))


@app.post("/api/register")
def register():
    start = time.perf_counter()
    body = request.get_json(silent=True) or {}
    name = (body.get("name") or "").strip()
    email = (body.get("email") or "").strip()
    company = (body.get("company") or "").strip()
    ticket_type = (body.get("ticketType") or "standard").strip()

    # Established only once validation passes; used for the metric attribute.
    validated_ticket_type = None

    try:
        # 1) validate-registration -------------------------------------------
        with tracer.start_as_current_span("validate-registration"):
            try:
                if not name:
                    raise RegistrationError(
                        "validation_error", 400, "Name is required"
                    )
                if not email or "@" not in email:
                    raise RegistrationError(
                        "validation_error", 400, "A valid email is required"
                    )
                if ticket_type not in ALLOWED_TICKET_TYPES:
                    raise RegistrationError(
                        "unknown_ticket_type",
                        400,
                        "Ticket type must be one of: standard, workshop, vip",
                    )
            except RegistrationError as exc:
                _fail_on_current_span(exc)
                raise

            # Validation passed: now safe to record the bounded ticket type and
            # the low-cardinality email domain (never the full address).
            validated_ticket_type = ticket_type
            span = otel_setup.trace.get_current_span()
            span.set_attribute("com.dutchcloudnativeday.registration.ticket_type", validated_ticket_type)
            span.set_attribute("com.dutchcloudnativeday.registration.email_domain", email.split("@", 1)[1])

        # 2) save-registration -----------------------------------------------
        with tracer.start_as_current_span("save-registration") as span:
            span.set_attribute("db.system.name", "postgresql")
            if random.random() < 0.10:
                # The "slow query" the workshop investigates as a latency outlier.
                # It shows up as this span's own duration (and in the
                # registration.duration metric) — no ad-hoc attribute needed.
                delay = random.uniform(0.400, 0.700)
            else:
                delay = random.uniform(0.020, 0.120)
            time.sleep(delay)
            registration_id = str(uuid.uuid4())
            span.set_attribute("com.dutchcloudnativeday.registration.id", registration_id)

        # 3) send-confirmation-email -----------------------------------------
        with tracer.start_as_current_span("send-confirmation-email"):
            time.sleep(random.uniform(0.015, 0.060))
            try:
                if "+fail" in email:
                    raise RegistrationError(
                        "invalid_recipient",
                        422,
                        "Email address was rejected by the provider (+fail)",
                    )
                if random.random() < 0.15:
                    raise RegistrationError(
                        "email_provider_unavailable",
                        503,
                        "Email provider is temporarily unavailable",
                    )
            except RegistrationError as exc:
                _fail_on_current_span(exc)
                raise

    except RegistrationError as exc:
        # Emit the failure as a correlated OTel log record (severity ERROR).
        # Note: we are no longer inside the child span's context here, but the
        # server span is still current, so the log stays on the same trace.
        logger.error(exc.message, exc_info=True)

        attrs = {"outcome": "failed", "error.type": exc.error_type}
        if validated_ticket_type is not None:
            attrs["com.dutchcloudnativeday.registration.ticket_type"] = validated_ticket_type
        registration_duration.record(time.perf_counter() - start, attributes=attrs)

        return (
            jsonify(
                {
                    "status": "error",
                    "errorType": exc.error_type,
                    "message": exc.message,
                }
            ),
            exc.status_code,
        )

    registration_duration.record(
        time.perf_counter() - start,
        attributes={
            "outcome": "confirmed",
            "com.dutchcloudnativeday.registration.ticket_type": validated_ticket_type,
        },
    )
    return jsonify({"status": "confirmed", "registrationId": registration_id}), 200


@app.get("/healthz")
def healthz():
    return "ok", 200


if __name__ == "__main__":
    port = int(os.environ.get("PORT", "3101"))
    # use_reloader off so the providers are not initialised twice.
    app.run(host="0.0.0.0", port=port, debug=False, use_reloader=False)
