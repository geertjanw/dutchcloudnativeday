"""Programmatic OpenTelemetry setup for the registration apps.

Import this module at the very top of app.py, BEFORE creating the Flask app.
It wires up all three signals against Dash0 over OTLP/HTTP, with the auth
headers set IN CODE from DASH0_AUTH_TOKEN / DASH0_DATASET (robust across SDKs):

  * Traces  — TracerProvider + BatchSpanProcessor + OTLP/HTTP span exporter.
  * Metrics — MeterProvider + PeriodicExportingMetricReader + OTLP/HTTP exporter.
  * Logs    — LoggerProvider + BatchLogRecordProcessor + OTLP/HTTP log exporter,
              bridged into the stdlib logging module via LoggingHandler.

It exposes:

  * ``tracer``             — a tracer for creating custom spans.
  * ``meter``              — a meter for creating instruments (histograms, ...).
  * ``logger``             — a stdlib logger whose records are exported as OTel
                             log records (and correlated to the active span).
  * ``instrument_app(app)``— call once with the Flask app to auto-instrument
    inbound Flask requests and the outbound ``requests`` HTTP client (the latter
    propagates W3C trace context so frontend -> backend is one distributed trace).
"""

import logging
import os

from opentelemetry import metrics, trace
from opentelemetry._logs import set_logger_provider
from opentelemetry.exporter.otlp.proto.http._log_exporter import OTLPLogExporter
from opentelemetry.exporter.otlp.proto.http.metric_exporter import OTLPMetricExporter
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
from opentelemetry.instrumentation.flask import FlaskInstrumentor
from opentelemetry.instrumentation.requests import RequestsInstrumentor
from opentelemetry.sdk._logs import LoggerProvider, LoggingHandler
from opentelemetry.sdk._logs.export import BatchLogRecordProcessor
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import PeriodicExportingMetricReader
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor

# --- Resource: carry service.name so Dash0 can group signals by service. -----
_service_name = os.environ.get("OTEL_SERVICE_NAME", "registration-python")
_resource = Resource.create({"service.name": _service_name})

# --- Shared OTLP/HTTP connection details (endpoint base + Dash0 headers). -----
_endpoint = os.environ.get(
    "OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4318"
).rstrip("/")
_headers = {
    "Authorization": "Bearer " + os.environ.get("DASH0_AUTH_TOKEN", ""),
    "Dash0-Dataset": os.environ.get("DASH0_DATASET", "default"),
}

# --- Traces ------------------------------------------------------------------
_span_exporter = OTLPSpanExporter(
    endpoint=_endpoint + "/v1/traces",
    headers=_headers,
)
_tracer_provider = TracerProvider(resource=_resource)
_tracer_provider.add_span_processor(BatchSpanProcessor(_span_exporter))
trace.set_tracer_provider(_tracer_provider)

# --- Metrics -----------------------------------------------------------------
_metric_exporter = OTLPMetricExporter(
    endpoint=_endpoint + "/v1/metrics",
    headers=_headers,
)
_metric_reader = PeriodicExportingMetricReader(_metric_exporter)
_meter_provider = MeterProvider(resource=_resource, metric_readers=[_metric_reader])
metrics.set_meter_provider(_meter_provider)

# --- Logs --------------------------------------------------------------------
_log_exporter = OTLPLogExporter(
    endpoint=_endpoint + "/v1/logs",
    headers=_headers,
)
_logger_provider = LoggerProvider(resource=_resource)
_logger_provider.add_log_record_processor(BatchLogRecordProcessor(_log_exporter))
set_logger_provider(_logger_provider)

# Bridge a stdlib logger into OTel: records emitted through it become OTel log
# records, and (when emitted inside a span) carry the trace/span ids so Dash0
# correlates the log to the span.
_otel_handler = LoggingHandler(level=logging.INFO, logger_provider=_logger_provider)
logger = logging.getLogger("registration")
logger.setLevel(logging.INFO)
logger.addHandler(_otel_handler)
logger.propagate = False

# Instrument the outbound HTTP client now so traceparent headers propagate.
RequestsInstrumentor().instrument()

# Module-level signal handles for the app handlers.
tracer = trace.get_tracer(_service_name)
meter = metrics.get_meter(_service_name)


def instrument_app(app):
    """Auto-instrument inbound Flask requests for the given app."""
    FlaskInstrumentor().instrument_app(app)
    return app
