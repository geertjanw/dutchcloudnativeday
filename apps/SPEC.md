# Shared contract for all registration apps

Every language implements the **same** two-service app so the workshop feels
identical no matter which stack an attendee picks. Read this file before
building or changing any app.

## Topology

```
browser ──> frontend (web, serves the form) ──HTTP──> backend (API, does the work)
                    │                                        │
                    └──────────── OpenTelemetry ─────────────┘
                                   (OTLP → Dash0)
```

Both services export traces to Dash0 over OTLP/HTTP. Context MUST propagate from
the frontend HTTP call into the backend so a single registration shows up as
**one end-to-end distributed trace spanning two services** in Dash0.

## Ports (fixed, so all four languages can run at once)

| Language | Frontend | Backend |
|----------|----------|---------|
| Node.js  | 3000     | 3001    |
| Python   | 3100     | 3101    |
| Go       | 3200     | 3201    |
| Java     | 3300     | 3301    |

## OpenTelemetry service names

| Language | Frontend `OTEL_SERVICE_NAME` | Backend `OTEL_SERVICE_NAME` |
|----------|------------------------------|-----------------------------|
| Node.js  | `registration-web-node`      | `registration-api-node`     |
| Python   | `registration-web-python`    | `registration-api-python`   |
| Go       | `registration-web-go`        | `registration-api-go`       |
| Java     | `registration-web-java`      | `registration-api-java`     |

## Environment variables

The root `.env` (copied from `.env.example`) holds the shared, vendor-neutral
OTLP settings plus the Dash0 token/dataset as plain values:

| Variable | Purpose |
|----------|---------|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Dash0 ingress base URL, e.g. `https://ingress.eu-west-1.aws.dash0.com` |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | Always `http/protobuf` |
| `DASH0_AUTH_TOKEN`            | The `auth_...` token |
| `DASH0_DATASET`              | Target dataset, e.g. `default` |

Each service additionally sets, in its own `run.sh`:

| Variable | Purpose |
|----------|---------|
| `OTEL_SERVICE_NAME` | Per-service name from the table above |
| `PORT`              | Port the service listens on (defaults per table) |
| `BACKEND_URL`       | Frontend only: base URL of the backend, default `http://localhost:<backend-port>` |

### How auth headers reach the exporter (IMPORTANT — do it this way)

Do **not** rely on sourcing `Authorization=Bearer <token>` through
`OTEL_EXPORTER_OTLP_HEADERS` in the shell — the space breaks `source`, and SDKs
disagree on URL-decoding that variable. Instead:

- **Node / Python / Go**: read `DASH0_AUTH_TOKEN` and `DASH0_DATASET` from the
  environment and set the OTLP exporter headers **in code**:
  `Authorization: Bearer <token>` and `Dash0-Dataset: <dataset>`.
- **Java** (uses the javaagent, no app code): in `run.sh`, build and export
  `OTEL_EXPORTER_OTLP_HEADERS="Authorization=Bearer ${DASH0_AUTH_TOKEN},Dash0-Dataset=${DASH0_DATASET}"`.

### Every service has a `run.sh`

It `cd`s to its own dir, sources the root `.env`, exports its `OTEL_SERVICE_NAME`
/ `PORT` (/ `BACKEND_URL`), then launches. Pattern:

```bash
#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
set -a; source ../../../.env; set +a      # repo-root/.env (service is at apps/<lang>/<service>)
export OTEL_SERVICE_NAME=registration-api-<lang>
export PORT=<port>
exec <launch command>
```

## Backend API contract

`POST /api/register` — request body JSON:

```json
{ "name": "Ada Lovelace", "email": "ada@example.com", "company": "Analytical Engines", "ticketType": "standard" }
```

`ticketType` is one of `standard`, `workshop`, `vip`.

The handler creates these child spans (in order) under the auto-created server span:

1. **`validate-registration`** — reject the request when:
   - name or email is empty, or email has no `@` → `error.type = validation_error`;
   - `ticketType` is not one of the allowed set `standard`, `workshop`, `vip`
     → `error.type = unknown_ticket_type`.

   Only **after** it passes, set `com.dutchcloudnativeday.registration.ticket_type`
   to the (now known-bounded) value. Set
   `com.dutchcloudnativeday.registration.email_domain` = the part after `@`.
   **Do not put the full email address on any span** — it is PII and unbounded;
   the domain is enough for the workshop and stays low-cardinality.

   > Custom attributes are namespaced with the reverse-DNS prefix
   > `com.dutchcloudnativeday.*` per OTel semantic conventions (bare,
   > single-segment names like `ticket_type` are invalid; a project namespace
   > keeps them from colliding with the semconv registry). This is the
   > workshop's "consistent naming conventions" point made concrete.
2. **`save-registration`** — simulate a DB write. Sleep a random 20–120 ms.
   With 10% probability sleep 400–700 ms instead (the "slow query" the workshop
   investigates as a latency outlier). Set attribute `db.system.name=postgresql`
   (the current semconv key; the older `db.system` was renamed).
3. **`send-confirmation-email`** — simulate calling an email provider. Sleep
   15–60 ms. **Deliberate failures (document these):**
   - If the email contains the substring `+fail`, the address is rejected as a
     bounced/invalid recipient (the *client's* problem) → `error.type =
     invalid_recipient`. This is the deterministic failure attendees trigger.
   - Otherwise, with 15% probability, the provider is transiently unavailable (a
     *server-side* outage) → `error.type = email_provider_unavailable`.

### Error model (status codes + `error.type`) — IMPORTANT

Distinguish a client/business failure from a server outage, and classify every
failure with a **bounded** `error.type` attribute (the stable OTel semconv key).
Never put a raw exception class name or message into `error.type` — it is a
metric/low-cardinality dimension. The full enum is exactly:

| Condition | `error.type` | HTTP status |
|-----------|-------------|-------------|
| name/email empty or malformed | `validation_error` | `400` |
| `ticketType` not in the allowed set | `unknown_ticket_type` | `400` |
| email contains `+fail` (bad recipient) | `invalid_recipient` | `422` |
| random flaky provider (~15%) | `email_provider_unavailable` | `503` |

Response body on any failure:

```json
{ "status": "error", "errorType": "<one of the enum values>", "message": "<human-readable>" }
```

**Record each failure exactly once.** Record the exception and set
`StatusCode.ERROR` + `error.type` **on the single child span where it occurs**
(`validate-registration` or `send-confirmation-email`). Do **not** also
re-record the same exception on the parent/server span — that double-counts
errors. Let the server span's status follow from the HTTP status as the
instrumentation already does.

Success response `200`:

```json
{ "status": "confirmed", "registrationId": "<uuid-or-random-id>" }
```

## Metrics — `registration.duration` histogram

The backend records **one** histogram measurement per `POST /api/register` call:

- Instrument: histogram named `registration.duration`, unit `s` (seconds),
  description "End-to-end registration handling time".
- Value: wall-clock duration of the whole handler.
- Attributes (all **bounded**, so the metric cannot explode in cardinality):
  - `com.dutchcloudnativeday.registration.ticket_type` — the validated ticket
    type, or omit if the request failed validation before a ticket type was
    established.
  - `outcome` — `confirmed` or `failed`.
  - `error.type` — present only when `outcome=failed`, one of the enum above.

Do **not** add `com.dutchcloudnativeday.registration.email_domain`, the
registration id, or anything unbounded to the metric — those belong on spans
only. This is the workshop's "keep metrics under control" point made concrete.

The metric **name** stays `registration.duration` (dot-namespaced, not a bare
attribute); only the custom *attribute keys* take the reverse-DNS prefix.

## Logs — exceptions as log records

On a failure, in addition to the span error, emit it as an **OTel log record**
(not only a span event) at severity `ERROR`, correlated to the current trace
(so Dash0 links the log to the span). Body = the human message; include
`error.type`. Keep it to failures; successes do not log. Each language note
below says how (bridge the stack's logger, or use the Logs SDK/agent).

## Frontend contract

- `GET /` — serve the registration page (HTML below, served verbatim).
- `POST /register` — read form fields `name`, `email`, `company`, `ticketType`,
  call the backend `POST /api/register` **with trace context propagated**, then
  render the result page (below). On success show the registration id. On
  failure show the backend's `message`, and **mirror the backend's status
  class** instead of flattening everything to one code: pass through the
  backend's status (`400`/`422`/`503`), and use `502` only when the backend is
  actually unreachable. This keeps the client/outage distinction visible.
- `GET /healthz` — return `200 ok` (no tracing needed).

The frontend's outbound HTTP client MUST be instrumented so `traceparent` is
sent to the backend (use the stack's auto-instrumentation or the OTel HTTP
transport — see each language's notes).

## Registration page HTML (`GET /`)

Serve this exactly (frontend may template the small result section separately).

```html
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Register · Dutch Cloud Native Day</title>
<style>
  :root { --ink:#0b1020; --accent:#ff6a13; --accent2:#1f6feb; --line:#e5e7eb; --muted:#5b6472; }
  * { box-sizing:border-box; }
  body { margin:0; font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Helvetica,Arial,sans-serif;
         background:linear-gradient(135deg,#0b1020,#13224a); color:var(--ink); min-height:100vh;
         display:flex; align-items:center; justify-content:center; padding:24px; }
  .card { background:#fff; border-radius:16px; box-shadow:0 20px 60px rgba(0,0,0,.35); width:100%; max-width:460px; overflow:hidden; }
  .head { background:linear-gradient(135deg,var(--accent),#ff8f4d); color:#fff; padding:28px 28px 22px; }
  .head h1 { margin:0; font-size:22px; letter-spacing:.2px; }
  .head p { margin:6px 0 0; opacity:.95; font-size:13px; }
  form { padding:24px 28px 28px; }
  label { display:block; font-size:13px; font-weight:600; margin:14px 0 6px; }
  input, select { width:100%; padding:11px 12px; border:1px solid var(--line); border-radius:9px; font-size:14px; }
  input:focus, select:focus { outline:2px solid var(--accent2); border-color:transparent; }
  button { margin-top:22px; width:100%; padding:13px; border:0; border-radius:9px; background:var(--accent2);
           color:#fff; font-size:15px; font-weight:600; cursor:pointer; }
  button:hover { background:#1a5fd0; }
  .hint { margin-top:16px; font-size:12px; color:var(--muted); line-height:1.5; }
  .hint code { background:#f1f3f5; padding:1px 5px; border-radius:4px; }
</style>
</head>
<body>
  <div class="card">
    <div class="head">
      <h1>Dutch Cloud Native Day</h1>
      <p>Register to attend · From One Request to One Trace</p>
    </div>
    <form method="post" action="/register">
      <label for="name">Full name</label>
      <input id="name" name="name" required placeholder="Ada Lovelace">
      <label for="email">Email</label>
      <input id="email" name="email" type="email" required placeholder="ada@example.com">
      <label for="company">Company</label>
      <input id="company" name="company" placeholder="Analytical Engines">
      <label for="ticketType">Ticket type</label>
      <select id="ticketType" name="ticketType">
        <option value="standard">Standard</option>
        <option value="workshop">Workshop</option>
        <option value="vip">VIP</option>
      </select>
      <button type="submit">Register</button>
      <p class="hint">Tip for the workshop: register with an email containing
        <code>+fail</code> (e.g. <code>ada+fail@example.com</code>) to force an
        error trace in Dash0. Otherwise ~15% of registrations fail at random.</p>
    </form>
  </div>
</body>
</html>
```

## Result page (`POST /register` response)

Reuse the same shell. Success:

```html
<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Registered · Dutch Cloud Native Day</title>
<style>/* same :root + body + card + head styles as above */</style></head>
<body><div class="card">
  <div class="head" style="background:linear-gradient(135deg,#16a34a,#22c55e)">
    <h1>You're registered 🎉</h1><p>Dutch Cloud Native Day</p></div>
  <div style="padding:24px 28px 28px;font-size:14px;line-height:1.6">
    <p>Thanks, <strong>{{name}}</strong>! A confirmation is on its way to
       <strong>{{email}}</strong>.</p>
    <p style="color:#5b6472">Registration id: <code>{{registrationId}}</code></p>
    <p><a href="/">← Register someone else</a></p>
  </div></div></body></html>
```

Error (HTTP 400/422/502/503 — mirror the backend's status). The `{{error}}`
placeholder shows the backend `message`; you may also show the `errorType`:

```html
<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Registration failed · Dutch Cloud Native Day</title>
<style>/* same :root + body + card + head styles as above */</style></head>
<body><div class="card">
  <div class="head" style="background:linear-gradient(135deg,#dc2626,#ef4444)">
    <h1>Registration failed</h1><p>Dutch Cloud Native Day</p></div>
  <div style="padding:24px 28px 28px;font-size:14px;line-height:1.6">
    <p>Something went wrong processing your registration.</p>
    <p style="color:#5b6472">Error: <code>{{error}}</code></p>
    <p>This is expected in the workshop — now go find this trace in Dash0 and
       see which span failed.</p>
    <p><a href="/">← Try again</a></p>
  </div></div></body></html>
```

## README requirements (one per language, in the language's folder)

Each README must let someone go from zero to a trace in Dash0 fast:
1. Prerequisites (runtime versions already verified: Node 20, Python 3.9+, Go
   1.22, Java 21 + Maven 3.9).
2. "Get your Dash0 credentials" — Settings → Auth Tokens for the token, and the
   Integration Hub for the fully-resolved OTLP endpoint; fill in root `.env`.
3. Install deps.
4. Run backend, then frontend (exact commands, env loading shown).
5. Open the frontend URL, register once, and (optionally) force a failure with
   `+fail`.
6. "See it in Dash0" — look for the two service names, open the trace, find the
   failing span, and note its bounded `error.type`. Mention the
   `registration.duration` metric (split by `ticket_type` / `error.type`) and
   that exceptions also arrive as correlated log records.
7. Keep it copy-pasteable. Assume the root `.env` is filled in.

## Per-language notes for metrics & logs (added in the refinement pass)

**Metrics** — backend emits the `registration.duration` histogram via the Metrics
SDK/API, exported over OTLP/HTTP to `${OTEL_EXPORTER_OTLP_ENDPOINT}/v1/metrics`
with the same auth headers as traces:
- **Node**: add a `PeriodicExportingMetricReader` with
  `@opentelemetry/exporter-metrics-otlp-proto` to the `NodeSDK`
  (`metricReader`), then `metrics.getMeter('registration').createHistogram(...)`.
- **Python**: build a `MeterProvider` with `PeriodicExportingMetricReader` +
  `OTLPMetricExporter` (http), `set_meter_provider`, then
  `get_meter(...).create_histogram(...)`.
- **Go**: add `otlpmetrichttp` + `sdkmetric.NewMeterProvider` with a
  `PeriodicReader`, `otel.SetMeterProvider`, then `meter.Float64Histogram(...)`.
- **Java**: the javaagent already wires a `MeterProvider` and exports metrics —
  just `GlobalOpenTelemetry.getMeter("registration").histogramBuilder(...)`.

**Logs** — backend emits the failure as an OTel log record:
- **Node**: add a `LoggerProvider` + `BatchLogRecordProcessor` with
  `@opentelemetry/exporter-logs-otlp-proto` (NodeSDK `logRecordProcessors`),
  then emit via the Logs API `logs.getLogger('registration').emit({...})` with
  severity ERROR (context is current, so trace correlation is automatic).
- **Python**: set up the Logs SDK `LoggerProvider` + `OTLPLogExporter` (http) +
  `BatchLogRecordProcessor`, attach the OTel `LoggingHandler` to a stdlib
  logger, and call `logger.error(msg, exc_info=True)` on failure.
- **Go**: use the Logs SDK (`go.opentelemetry.io/otel/log`, `otlploghttp`,
  `sdk/log`) with the `otelslog` bridge; log the error via `slog` within the
  request context.
- **Java**: the javaagent's log appender exports Logback/SLF4J logs over OTLP by
  default — just `log.error("...", exception)`; no code wiring needed.

**Maven wrapper (Java only)**: add the Maven wrapper (`mvn -N wrapper:wrapper`)
to both Java projects and use `./mvnw` in `run.sh` and the README so the app
builds without a system Maven. `.mvn/wrapper/` and `mvnw`/`mvnw.cmd` are kept
(do not gitignore them).
