'use strict';

const express = require('express');
const crypto = require('crypto');
const { trace, context, metrics, SpanStatusCode } = require('@opentelemetry/api');
const { logs, SeverityNumber } = require('@opentelemetry/api-logs');

const app = express();
app.use(express.json());

const tracer = trace.getTracer('registration');
const meter = metrics.getMeter('registration');
const logger = logs.getLogger('registration');

// One histogram measurement per /api/register call. All attributes are bounded.
const registrationDuration = meter.createHistogram('registration.duration', {
  unit: 's',
  description: 'End-to-end registration handling time',
});

const PORT = process.env.PORT || 3001;
const ALLOWED_TICKET_TYPES = new Set(['standard', 'workshop', 'vip']);

// Bounded error.type enum → HTTP status mapping (see SPEC error model).
const ERROR_STATUS = {
  validation_error: 400,
  unknown_ticket_type: 400,
  invalid_recipient: 422,
  email_provider_unavailable: 503,
};

// A classified failure. `errorType` is one of the bounded enum values above.
class RegistrationError extends Error {
  constructor(errorType, message) {
    super(message);
    this.name = 'RegistrationError';
    this.errorType = errorType;
  }
}

// Helper: sleep for the given number of milliseconds.
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

// Run a function inside a child span, ending the span afterwards.
// On a RegistrationError, record the exception + ERROR status + error.type on
// THIS child span only (never the parent/server span — avoids double-counting).
async function withSpan(name, fn) {
  const span = tracer.startSpan(name);
  const ctx = trace.setSpan(context.active(), span);
  try {
    return await context.with(ctx, () => fn(span));
  } catch (err) {
    span.recordException(err);
    span.setStatus({ code: SpanStatusCode.ERROR, message: err.message });
    if (err instanceof RegistrationError) {
      span.setAttribute('error.type', err.errorType);
    }
    throw err;
  } finally {
    span.end();
  }
}

app.get('/healthz', (_req, res) => {
  res.status(200).send('ok');
});

app.post('/api/register', async (req, res) => {
  const start = process.hrtime.bigint();
  const { name, email, company, ticketType } = req.body || {};

  // ticket type is only a bounded metric dimension once validation has
  // confirmed it is in the allowed set; undefined means "not yet established".
  let validatedTicketType;

  try {
    // 1) validate-registration
    await withSpan('validate-registration', async (span) => {
      if (!name || !String(name).trim()) {
        throw new RegistrationError('validation_error', 'name is required');
      }
      if (!email || !String(email).trim()) {
        throw new RegistrationError('validation_error', 'email is required');
      }
      if (!String(email).includes('@')) {
        throw new RegistrationError('validation_error', 'email is invalid');
      }
      if (!ALLOWED_TICKET_TYPES.has(ticketType)) {
        throw new RegistrationError(
          'unknown_ticket_type',
          `ticketType must be one of standard, workshop, vip (got "${ticketType}")`
        );
      }
      // Only AFTER validation passes: set bounded ticket type + email domain.
      // The full email address is deliberately NOT recorded (PII, unbounded).
      validatedTicketType = ticketType;
      span.setAttribute('com.dutchcloudnativeday.registration.ticket_type', ticketType);
      span.setAttribute('com.dutchcloudnativeday.registration.email_domain', String(email).split('@')[1] || '');
    });

    // 2) save-registration — simulate a DB write.
    const registrationId = await withSpan('save-registration', async (span) => {
      span.setAttribute('db.system.name', 'postgresql');
      // 10% of the time this is the "slow query" latency outlier.
      const slow = Math.random() < 0.1;
      const delay = slow
        ? 400 + Math.floor(Math.random() * 300) // 400–700 ms
        : 20 + Math.floor(Math.random() * 100); // 20–120 ms
      // The slow case is visible as this span's own duration (and in the
      // registration.duration metric) — no ad-hoc attribute needed.
      await sleep(delay);
      return crypto.randomUUID();
    });

    // 3) send-confirmation-email — simulate calling an email provider.
    await withSpan('send-confirmation-email', async (span) => {
      span.setAttribute('messaging.system', 'email');
      await sleep(15 + Math.floor(Math.random() * 46)); // 15–60 ms
      // Deterministic client-side failure: any email containing "+fail".
      if (String(email).includes('+fail')) {
        throw new RegistrationError(
          'invalid_recipient',
          'the recipient address was rejected as invalid (forced +fail)'
        );
      }
      // Transient server-side outage: 15% random failure.
      if (Math.random() < 0.15) {
        throw new RegistrationError(
          'email_provider_unavailable',
          'the email provider is temporarily unavailable'
        );
      }
    });

    recordDuration(start, { outcome: 'confirmed', ticketType: validatedTicketType });
    res.status(200).json({ status: 'confirmed', registrationId });
  } catch (err) {
    const errorType = err instanceof RegistrationError ? err.errorType : 'validation_error';
    const status = ERROR_STATUS[errorType] || 500;

    // Emit the failure as an OTel log record, correlated to the current span
    // (context is still active here). Successes do not log.
    logger.emit({
      severityNumber: SeverityNumber.ERROR,
      severityText: 'ERROR',
      body: err.message,
      attributes: { 'error.type': errorType },
    });

    recordDuration(start, { outcome: 'failed', ticketType: validatedTicketType, errorType });
    res.status(status).json({ status: 'error', errorType, message: err.message });
  }
});

// Record the one histogram measurement with bounded attributes.
function recordDuration(start, { outcome, ticketType, errorType }) {
  const seconds = Number(process.hrtime.bigint() - start) / 1e9;
  const attrs = { outcome };
  if (ticketType) attrs['com.dutchcloudnativeday.registration.ticket_type'] = ticketType;
  if (outcome === 'failed' && errorType) attrs['error.type'] = errorType;
  registrationDuration.record(seconds, attrs);
}

app.listen(PORT, () => {
  console.log(`[registration-api] listening on :${PORT}`);
});
