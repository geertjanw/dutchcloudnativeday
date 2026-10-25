'use strict';

// OpenTelemetry bootstrap. Loaded BEFORE the app via `node -r ./tracing.js server.js`
// so the auto-instrumentations patch http/express/undici before they are required.
//
// Auth headers are built in code from DASH0_AUTH_TOKEN / DASH0_DATASET (see SPEC):
// sourcing `Authorization=Bearer ...` through OTEL_EXPORTER_OTLP_HEADERS is fragile,
// so we set the exporter headers here instead.
//
// This wires all three signals to Dash0 over OTLP/HTTP (protobuf):
//   traces  -> ${endpoint}/v1/traces   (NodeSDK traceExporter)
//   metrics -> ${endpoint}/v1/metrics  (PeriodicExportingMetricReader)
//   logs    -> ${endpoint}/v1/logs     (BatchLogRecordProcessor)
// All exporters fail silently when the collector is unreachable — the app must
// never crash because of a failed export.

const { NodeSDK } = require('@opentelemetry/sdk-node');
const { getNodeAutoInstrumentations } = require('@opentelemetry/auto-instrumentations-node');
const { OTLPTraceExporter } = require('@opentelemetry/exporter-trace-otlp-proto');
const { OTLPMetricExporter } = require('@opentelemetry/exporter-metrics-otlp-proto');
const { OTLPLogExporter } = require('@opentelemetry/exporter-logs-otlp-proto');
const { PeriodicExportingMetricReader } = require('@opentelemetry/sdk-metrics');
const { BatchLogRecordProcessor } = require('@opentelemetry/sdk-logs');
const { resourceFromAttributes } = require('@opentelemetry/resources');
const { ATTR_SERVICE_NAME } = require('@opentelemetry/semantic-conventions');

const endpoint = process.env.OTEL_EXPORTER_OTLP_ENDPOINT || 'http://localhost:4318';
const serviceName = process.env.OTEL_SERVICE_NAME || 'registration-node';

// Same auth headers on every signal.
const headers = {
  Authorization: 'Bearer ' + (process.env.DASH0_AUTH_TOKEN || ''),
  'Dash0-Dataset': process.env.DASH0_DATASET || 'default',
};

const traceExporter = new OTLPTraceExporter({
  url: `${endpoint}/v1/traces`,
  headers,
});

const metricReader = new PeriodicExportingMetricReader({
  exporter: new OTLPMetricExporter({
    url: `${endpoint}/v1/metrics`,
    headers,
  }),
});

const logRecordProcessor = new BatchLogRecordProcessor(
  new OTLPLogExporter({
    url: `${endpoint}/v1/logs`,
    headers,
  })
);

const sdk = new NodeSDK({
  // NodeSDK already reads OTEL_SERVICE_NAME, but set it explicitly too for clarity.
  resource: resourceFromAttributes({
    [ATTR_SERVICE_NAME]: serviceName,
  }),
  traceExporter,
  metricReader,
  logRecordProcessors: [logRecordProcessor],
  // Auto-instruments express, http AND undici/global fetch so trace context
  // propagates from the frontend fetch() call into the backend.
  instrumentations: [getNodeAutoInstrumentations()],
});

// Start synchronously at require time.
sdk.start();

// Flush spans/metrics/logs on shutdown. Export failures (e.g. unreachable
// collector) must never crash the app — swallow them.
process.on('SIGTERM', () => {
  sdk.shutdown().catch(() => {}).finally(() => process.exit(0));
});
process.on('SIGINT', () => {
  sdk.shutdown().catch(() => {}).finally(() => process.exit(0));
});
