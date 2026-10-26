# Registration app — Python (Flask + OpenTelemetry)

The Python implementation of the Dutch Cloud Native Day workshop app:
**"From One Request to One Trace."** Two Flask services export OTLP/HTTP traces
to Dash0, and trace context propagates frontend → backend so a single
registration shows up as **one end-to-end distributed trace across two
services**.

| Service    | Dir         | `OTEL_SERVICE_NAME`        | Port |
|------------|-------------|----------------------------|------|
| Frontend   | `frontend/` | `registration-web-python`  | 3100 |
| Backend    | `backend/`  | `registration-api-python`  | 3101 |

The frontend serves the registration form and POSTs to the backend
`POST /api/register`, which runs three child spans — `validate-registration`,
`save-registration`, `send-confirmation-email` — under the auto-created server
span. OpenTelemetry is configured **programmatically** in `otel_setup.py` (no
`opentelemetry-instrument` agent), so the Dash0 auth headers are set in code and
survive shell quirks. All three signals go to Dash0 over OTLP/HTTP:

- **Traces** — the distributed trace across both services.
- **Metrics** — the backend records one `registration.duration` histogram
  (unit `s`) per request, with bounded attributes `com.dutchcloudnativeday.registration.ticket_type`,
  `outcome` (`confirmed`/`failed`) and, on failure, `error.type`.
- **Logs** — on a failure the backend emits one correlated OTel log record at
  severity `ERROR` (bridged from the stdlib logger); successes do not log.

### Error model

Each failure is classified with a bounded `error.type` and mapped to a status:

| Condition | `error.type` | HTTP |
|-----------|--------------|------|
| name/email empty or malformed | `validation_error` | 400 |
| `ticketType` not `standard`/`workshop`/`vip` | `unknown_ticket_type` | 400 |
| email contains `+fail` (bad recipient) | `invalid_recipient` | 422 |
| flaky provider (~15% random) | `email_provider_unavailable` | 503 |

Failure body: `{"status":"error","errorType":"...","message":"..."}`. The error
is recorded exactly once, on the single child span where it occurs. The full
email address is never put on telemetry — only `com.dutchcloudnativeday.registration.email_domain`.

## 1. Prerequisites

- **Python 3.9+** (verified on Python 3.9.6 / pip 21).
- A Dash0 account.

## 2. Get your Dash0 credentials

Fill in the repo-root `.env` (one file shared by every language). From the repo
root:

```bash
cp .env.example .env
```

Then edit `.env`:

- **`DASH0_AUTH_TOKEN`** — Dash0 app → **Settings → Auth Tokens** → create a
  token (starts with `auth_`), or use the **Auto-generated auth token**.
  <p><img src="../../images/general/auth-token-v2.png" alt="Dash0 Auth Tokens settings" width="900"></p>
- **`OTEL_EXPORTER_OTLP_ENDPOINT`** — Dash0 app → **Settings → Endpoints → OTLP
  via gRPC** shows your fully-resolved, region-specific ingress URL, e.g.
  `https://ingress.eu-west-1.aws.dash0.com`.
  <p><img src="../../images/general/endpoint-v2.png" alt="Dash0 OTLP endpoint settings" width="900"></p>
- **`DASH0_DATASET`** — target dataset (`default` is auto-created).
- `OTEL_EXPORTER_OTLP_PROTOCOL` stays `http/protobuf`.

Each service's `run.sh` sources this root `.env`, and `otel_setup.py` builds the
exporter headers (`Authorization: Bearer <token>`, `Dash0-Dataset: <dataset>`)
from it in code.

## 3. Install dependencies

Each service uses its own isolated virtualenv. Run once per service:

```bash
# Backend
cd apps/python/backend
python3 -m venv venv
./venv/bin/pip install -r requirements.txt

# Frontend
cd ../frontend
python3 -m venv venv
./venv/bin/pip install -r requirements.txt
```

## 4. Run the services

Open **two terminals** from the repo root. `run.sh` activates the venv, sources
the root `.env`, exports the per-service `OTEL_SERVICE_NAME` / `PORT`
(/ `BACKEND_URL`), and runs `python app.py`.

Terminal 1 — backend first:

```bash
cd apps/python/backend
./run.sh
# → registration-api-python on http://localhost:3101
```

Terminal 2 — frontend:

```bash
cd apps/python/frontend
./run.sh
# → registration-web-python on http://localhost:3100
```

Health checks: `curl http://localhost:3101/healthz` and
`curl http://localhost:3100/healthz` both return `ok`.

## 5. Register

Open **http://localhost:3100**, fill in the form, and submit. You'll see the
"You're registered 🎉" page with a registration id.

**Force a failure:** register with an email containing `+fail` (e.g.
`ada+fail@example.com`). The `send-confirmation-email` span fails
deterministically with `error.type = invalid_recipient` (HTTP 422) and you get
the red "Registration failed" page. Otherwise ~15% of registrations fail at
random (`email_provider_unavailable`, HTTP 503), and ~10% hit a slow
"save-registration" DB write (400–700 ms) you can spot as a latency outlier.

## 6. See it in Dash0

1. In Dash0, open **Tracing**. You'll see two services:
   **`registration-web-python`** (frontend) and **`registration-api-python`**
   (backend).
2. Open a trace from a registration. It spans **both services** as one
   distributed trace: the frontend server span → the outbound HTTP call →
   the backend server span → the three child spans.
3. For a `+fail` registration, open the trace and find the red
   **`send-confirmation-email`** span. It carries the recorded exception, an
   `ERROR` status, and the bounded `error.type` (`invalid_recipient`) — that's
   the span that failed.
4. Open **Metrics** and chart **`registration.duration`**; split by
   `com.dutchcloudnativeday.registration.ticket_type` and `error.type` to compare outcomes and spot the
   slow-query outliers.
5. Open **Logs**: the failure also arrives as an `ERROR` log record carrying
   `error.type`, correlated to the same trace, so you can jump log → span.

## Notes

- Run with plain `python app.py` (`run.sh` does this). The reloader is disabled
  (`use_reloader=False`) so the tracer provider is not initialised twice.
- If the OTLP collector/Dash0 is unreachable, the app keeps serving requests;
  the span/log batch processors and the periodic metric reader just fail the
  export in the background without affecting the request.
- On macOS system Python you may see a harmless `NotOpenSSLWarning` from urllib3
  (LibreSSL). It does not affect the app.
