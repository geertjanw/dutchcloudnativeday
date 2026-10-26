# Registration app — Go

Two services that together form **one end-to-end distributed trace** per
registration, exported to [Dash0](https://www.dash0.com) over OTLP/HTTP:

| Service  | Dir         | Port | `OTEL_SERVICE_NAME`    |
|----------|-------------|------|------------------------|
| Frontend | `frontend/` | 3200 | `registration-web-go`  |
| Backend  | `backend/`  | 3201 | `registration-api-go`  |

The browser posts the form to the **frontend**, which calls the **backend**
`POST /api/register` with the W3C `traceparent` header propagated, so a single
registration shows up as one trace spanning both services in Dash0.

OpenTelemetry is wired up **manually** (no Go runtime auto-instrumentation):
each service builds an OTLP/HTTP exporter in code and uses
`otelhttp` to instrument both its HTTP server and the frontend's outbound
HTTP client.

## 1. Prerequisites

- **Go 1.22** (`go version` → `go1.22.x`). Verified on darwin/arm64.
- A Dash0 account (free trial is fine).

## 2. Get your Dash0 credentials

Fill in the **repo-root** `.env` (not this folder). From the repo root:

```bash
cp .env.example .env
```

Then edit `.env`:

- **Auth token** — Dash0 app → **Settings → Auth Tokens** → create a token
  (`auth_...`), or use the **Auto-generated auth token**. Put it in
  `DASH0_AUTH_TOKEN`.
  <p><img src="../../images/general/auth-token-v2.png" alt="Dash0 Auth Tokens settings" width="900"></p>
- **OTLP endpoint** — Dash0 app → **Settings → Endpoints → OTLP via gRPC** shows
  your fully-resolved, region-specific ingress URL, e.g.
  `https://ingress.eu-west-1.aws.dash0.com`. Put it in
  `OTEL_EXPORTER_OTLP_ENDPOINT`.
  <p><img src="../../images/general/endpoint-v2.png" alt="Dash0 OTLP endpoint settings" width="900"></p>
- `DASH0_DATASET` — leave as `default` unless you created another dataset.
- `OTEL_EXPORTER_OTLP_PROTOCOL` stays `http/protobuf`.

The auth headers (`Authorization: Bearer <token>` and `Dash0-Dataset`) are set
**in code** from `DASH0_AUTH_TOKEN` / `DASH0_DATASET` — you do not need to touch
`OTEL_EXPORTER_OTLP_HEADERS`.

## 3. Install dependencies

Each service is its own Go module. From this folder:

```bash
(cd backend  && go mod tidy && go build ./...)
(cd frontend && go mod tidy && go build ./...)
```

## 4. Run it — backend first, then frontend (two terminals)

Each service has a `run.sh` that `cd`s to its own dir, sources the repo-root
`.env`, exports its `OTEL_SERVICE_NAME` / `PORT` (and `BACKEND_URL` for the
frontend), then launches with `go run .`.

Terminal 1 — backend (`registration-api-go`, port 3201):

```bash
./backend/run.sh
```

Terminal 2 — frontend (`registration-web-go`, port 3200):

```bash
./frontend/run.sh
```

Health checks: `curl localhost:3201/healthz` and `curl localhost:3200/healthz`
both return `ok`.

## 5. Register

Open **http://localhost:3200** and submit the form. You'll get a confirmation
page with a registration id.

**Force a failure (for the workshop):** register with an email containing
`+fail`, e.g. `ada+fail@example.com`. This always fails in the
`send-confirmation-email` span (`error.type = invalid_recipient`, HTTP `422`)
and renders the red error page. (Even without `+fail`, ~15% of registrations hit
a transient `email_provider_unavailable` outage → HTTP `503`, and ~10% of saves
are an artificial "slow query" of 400–700 ms.)

### Failure model (bounded `error.type` + HTTP status)

| Condition | `error.type` | HTTP | Span |
|-----------|-------------|------|------|
| name/email empty or malformed | `validation_error` | `400` | `validate-registration` |
| `ticketType` not standard/workshop/vip | `unknown_ticket_type` | `400` | `validate-registration` |
| email contains `+fail` (bad recipient) | `invalid_recipient` | `422` | `send-confirmation-email` |
| random flaky provider (~15%) | `email_provider_unavailable` | `503` | `send-confirmation-email` |

The backend responds `{"status":"error","errorType":...,"message":...}`; the
frontend mirrors the backend's status (`400`/`422`/`503`) and uses `502` only
when the backend itself is unreachable.

## 6. See it in Dash0

1. Open Dash0 and go to **Tracing**.
2. You'll see two services: **`registration-web-go`** and
   **`registration-api-go`**.
3. Open a registration trace — it spans both services: the web server span, the
   outbound HTTP call, the backend server span, and its child spans
   **`validate-registration`**, **`save-registration`**, and
   **`send-confirmation-email`**.
4. For a `+fail` registration, open the trace and find the failing
   **`send-confirmation-email`** span — it is marked **error**, carries the
   recorded exception, and a bounded **`error.type`** attribute
   (`invalid_recipient`). The error is recorded only on that one child span (no
   double-counting on the server span). The slow-query traces show
   `save-registration` as a latency outlier.
5. **Metrics** — the backend emits one **`registration.duration`** histogram
   (unit `s`) per request. Split it by **`com.dutchcloudnativeday.registration.ticket_type`**,
   **`outcome`** (`confirmed`/`failed`), and **`error.type`** to see failure
   rates and the slow-query tail. Only these bounded attributes are on the
   metric — never the email or registration id.
6. **Logs** — each failure is also emitted as a correlated **ERROR log record**
   (body = the human message, with `error.type`). Open the failing span and
   follow the link to its log. Successes do not log.

## Notes

- If the Dash0 collector is unreachable the services still run — traces,
  metrics, and logs export is best-effort (batched / periodic) and export errors
  are logged, not fatal.
- **No PII in telemetry:** the full email address is never put on a span. Spans
  carry only `com.dutchcloudnativeday.registration.email_domain` (the part after `@`), kept
  low-cardinality. The attendee email is shown in the web UI only.
- OTel module versions are pinned in each `go.mod` to a mutually compatible set
  (otel core `v1.30.0`, otelhttp contrib `v0.55.0`, logs/log-bridge `v0.x`
  matching). Run `go mod tidy` to resolve.
