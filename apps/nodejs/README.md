# Registration app — Node.js

The Node.js implementation of the Dutch Cloud Native Day registration app:
two services (a web **frontend** and an API **backend**) wired with
OpenTelemetry so a single registration shows up in Dash0 as **one end-to-end
distributed trace spanning both services**.

| Service  | Role                         | Port | `OTEL_SERVICE_NAME`     |
|----------|------------------------------|------|-------------------------|
| frontend | Serves the form, calls the API | 3000 | `registration-web-node` |
| backend  | Validates, saves, emails     | 3001 | `registration-api-node` |

See [`../SPEC.md`](../SPEC.md) for the shared contract all languages implement.

## 1. Prerequisites

- **Node.js 20** and **npm 10** (verified with Node v20.20, npm 10.8). Node 20
  ships a global `fetch` (undici), which the frontend uses to call the backend.
- A Dash0 account (free trial is fine).

## 2. Get your Dash0 credentials

1. **Auth token:** Dash0 app → **Settings → Auth Tokens** → create a token
   (starts with `auth_...`).
2. **OTLP endpoint:** Dash0 app → **Integration Hub → OpenTelemetry** shows your
   fully-resolved, region-specific ingress URL, e.g.
   `https://ingress.eu-west-1.aws.dash0.com`.

Fill these into the **root** `.env` (shared by every app in this repo):

```bash
cd ../..                 # repo root
cp .env.example .env      # if you haven't already
# then edit .env:
#   OTEL_EXPORTER_OTLP_ENDPOINT=https://ingress.<region>.aws.dash0.com
#   OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
#   DASH0_AUTH_TOKEN=auth_...
#   DASH0_DATASET=default
```

**How the root `.env` is used:** each service's `run.sh` `cd`s into its own
directory, `source`s the repo-root `.env` (so the shared OTLP settings and your
Dash0 token/dataset are in the environment), then exports its own
`OTEL_SERVICE_NAME` / `PORT` (and `BACKEND_URL` for the frontend) before
launching. The OTLP **auth headers** (`Authorization: Bearer <token>` and
`Dash0-Dataset: <dataset>`) are built **in code** in `tracing.js` from
`DASH0_AUTH_TOKEN` / `DASH0_DATASET` — this is more robust than sourcing
`OTEL_EXPORTER_OTLP_HEADERS` through the shell.

## 3. Install dependencies

```bash
cd backend && npm install && cd ..
cd frontend && npm install && cd ..
```

## 4. Run it — backend first, then frontend (two terminals)

Each service is launched with `node -r ./tracing.js server.js` so that
OpenTelemetry's auto-instrumentations patch `express`, `http`, and
`undici`/`fetch` **before** the app loads. The `run.sh` scripts do this for you.

**Terminal 1 — backend:**

```bash
cd backend
./run.sh          # OTEL_SERVICE_NAME=registration-api-node, PORT=3001
```

**Terminal 2 — frontend:**

```bash
cd frontend
./run.sh          # OTEL_SERVICE_NAME=registration-web-node, PORT=3000, BACKEND_URL=http://localhost:3001
```

(If you prefer npm: `npm start` runs the same command, but then you must export
`OTEL_SERVICE_NAME`/`PORT`/`BACKEND_URL` and source `.env` yourself — `run.sh`
is the copy-pasteable path.)

## 5. Register

Open **http://localhost:3000** and submit the form.

- A normal registration succeeds (~15% fail at random — the "flaky email
  provider" the workshop investigates).
- **Force a failure on demand:** use an email containing `+fail`, e.g.
  `ada+fail@example.com`. The `send-confirmation-email` span will always fail,
  the page shows "Registration failed", and you get a guaranteed error trace.

## 6. See it in Dash0

1. Open Dash0 and go to **Tracing**.
2. You'll see two services: **`registration-web-node`** (frontend) and
   **`registration-api-node`** (backend).
3. Open a registration trace — it starts at the frontend `POST /register`,
   crosses into the backend `POST /api/register` (context propagated via the
   `traceparent` header on the `fetch` call), and fans out into three child
   spans: `validate-registration`, `save-registration`, `send-confirmation-email`.
4. For a failing registration, the single originating child span is marked as
   **error** — it carries the recorded exception, an ERROR status, and a
   **bounded `error.type`** attribute (never a raw message). The failure is
   recorded on exactly one span, not re-counted on the parent:
   - `+fail` email → `invalid_recipient` (HTTP 422) on `send-confirmation-email`;
   - random ~15% flaky provider → `email_provider_unavailable` (HTTP 503) on
     `send-confirmation-email`;
   - empty/malformed name or email → `validation_error` (HTTP 400) on
     `validate-registration`;
   - `ticketType` not in `{standard,workshop,vip}` → `unknown_ticket_type`
     (HTTP 400) on `validate-registration`.

   Roughly 10% of `save-registration` spans are a slow 400–700 ms "slow query"
   outlier; the rest are 20–120 ms. Spans carry `com.dutchcloudnativeday.registration.email_domain`
   (the part after `@`) but never the full email — PII stays out of telemetry.
5. **Metric:** the backend records one `registration.duration` histogram (unit
   seconds) per request, with bounded attributes `outcome`
   (`confirmed`/`failed`), `com.dutchcloudnativeday.registration.ticket_type`, and `error.type` (on
   failures). Split it by `com.dutchcloudnativeday.registration.ticket_type` or `error.type` in Dash0 —
   the "slow query" shows up as a long tail, and failures group by `error.type`.
6. **Logs:** every failure is also emitted as an OTel **log record** at severity
   ERROR, correlated to the trace/span (Dash0 links the log to the failing
   span). Successes do not log.

All three signals (traces, metrics, logs) export over OTLP/HTTP to Dash0 using
the same Bearer + `Dash0-Dataset` headers built in `tracing.js`.

## Project layout

```
apps/nodejs/
├── backend/
│   ├── tracing.js      # OTel bootstrap: NodeSDK + OTLP/proto trace, metric & log exporters + auto-instrumentations
│   ├── server.js       # Express API: POST /api/register (error model + registration.duration metric + error logs), GET /healthz
│   ├── package.json
│   └── run.sh
├── frontend/
│   ├── tracing.js      # same OTel bootstrap
│   ├── server.js       # Express web: GET /, POST /register, GET /healthz
│   ├── package.json
│   └── run.sh
└── README.md
```
