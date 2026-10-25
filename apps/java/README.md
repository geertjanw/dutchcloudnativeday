# Dutch Cloud Native Day — Registration app (Java)

Two Spring Boot services that together form **one end-to-end distributed trace**
in Dash0:

- **`registration-web-java`** (frontend, port **3300**) — serves the registration
  form and proxies submissions to the backend.
- **`registration-api-java`** (backend, port **3301**) — validates, "saves", and
  "emails" the registration, emitting three child spans.

Both services are instrumented by the **OpenTelemetry Java agent** (a
`-javaagent`, no OTel wiring in app code). The agent creates the server spans and
instruments the frontend's outbound HTTP client, so `traceparent` propagates
frontend → backend and the whole registration shows up as a single trace.

The backend additionally creates three **manual child spans** —
`validate-registration`, `save-registration`, `send-confirmation-email` — using
the OpenTelemetry API; the agent wires `GlobalOpenTelemetry` for these. It also
records a `registration.duration` histogram (via the agent's MeterProvider) and
logs failures through SLF4J, which the agent exports as OTLP log records
correlated to the trace.

Failures are classified with a bounded `error.type` and mapped to HTTP status:

| Condition | `error.type` | HTTP |
|-----------|--------------|------|
| empty/malformed name or email | `validation_error` | 400 |
| `ticketType` not in {standard, workshop, vip} | `unknown_ticket_type` | 400 |
| email contains `+fail` (bad recipient) | `invalid_recipient` | 422 |
| flaky provider (~15% random) | `email_provider_unavailable` | 503 |

The frontend mirrors the backend status (400/422/503) and uses 502 only when the
backend is unreachable.

---

## 1. Prerequisites

- **Java 21** (OpenJDK) — `java -version`
- **Maven 3.9** — `mvn -version`
- `curl` (to download the agent)

## 2. Get your Dash0 credentials

In the repo root, copy the example env file and fill it in:

```bash
cp .env.example .env      # run from the repository root
```

- **Auth token**: Dash0 app → **Settings → Auth Tokens** → create a token
  (`auth_...`) → put it in `DASH0_AUTH_TOKEN`.
- **OTLP endpoint**: Dash0 app → **Integration Hub → OpenTelemetry** shows your
  fully-resolved, region-specific ingress URL
  (e.g. `https://ingress.eu-west-1.aws.dash0.com`) → put it in
  `OTEL_EXPORTER_OTLP_ENDPOINT`.
- `OTEL_EXPORTER_OTLP_PROTOCOL` stays `http/protobuf`.
- `DASH0_DATASET` stays `default` (auto-created) unless you use another dataset.

The `run.sh` scripts source this root `.env` and build
`OTEL_EXPORTER_OTLP_HEADERS` (`Authorization=Bearer <token>,Dash0-Dataset=<dataset>`)
for the agent automatically.

## 3. Download the OpenTelemetry Java agent

From this folder (`apps/java`):

```bash
./download-agent.sh
```

This fetches the latest `opentelemetry-javaagent.jar` into `apps/java/`. Both
`run.sh` scripts load it as `../opentelemetry-javaagent.jar`.

## 4. Build

Each project ships the **Maven wrapper** (`./mvnw`), so no system Maven is
required:

```bash
# from apps/java
( cd backend  && ./mvnw -q package -DskipTests )
( cd frontend && ./mvnw -q package -DskipTests )
```

(The `run.sh` scripts also build automatically with `./mvnw` if the jar is
missing.)

## 5. Run — backend first, then frontend (two terminals)

**Terminal 1 — backend:**

```bash
cd apps/java/backend
./run.sh      # OTEL_SERVICE_NAME=registration-api-java, listens on :3301
```

**Terminal 2 — frontend:**

```bash
cd apps/java/frontend
./run.sh      # OTEL_SERVICE_NAME=registration-web-java, listens on :3300
```

Each `run.sh` sources the root `.env`, exports its `OTEL_SERVICE_NAME` / `PORT`
(the frontend also sets `BACKEND_URL`, default `http://localhost:3301`), then
launches the jar with the javaagent.

Health checks:

```bash
curl localhost:3301/healthz    # backend  -> ok
curl localhost:3300/healthz    # frontend -> ok
```

## 6. Register

Open **http://localhost:3300** and submit the form once — you'll get the green
"You're registered 🎉" page with a registration id.

**Force a failure (for the workshop):** register with an email containing
`+fail`, e.g. `ada+fail@example.com`. The backend always fails the
`send-confirmation-email` step for those with `error.type=invalid_recipient`,
returns HTTP **422**, and the frontend mirrors that status and shows the red
"Registration failed" page with the backend's message. (Even without `+fail`,
~15% of registrations fail at random with `email_provider_unavailable` → **503**,
and ~10% of saves are artificially slow.) A bad `ticketType` →
`unknown_ticket_type` → **400**; an empty name/email → `validation_error` →
**400**.

## 7. See it in Dash0

- In Dash0, look for the two services **`registration-web-java`** and
  **`registration-api-java`**.
- Open a registration trace: it spans both services (frontend request →
  backend `POST /api/register`) as **one** trace thanks to context propagation.
- Under the backend server span you'll see the three child spans. For a `+fail`
  registration, the **`send-confirmation-email`** span is marked **ERROR** and
  carries the recorded exception and bounded `error.type=invalid_recipient` —
  that's the failing span to investigate. (Validation failures surface the same
  way on **`validate-registration`**.)
- **Metrics:** the `registration.duration` histogram (unit `s`) is recorded once
  per request; split it by `com.dutchcloudnativeday.registration.ticket_type`, `outcome`
  (`confirmed`/`failed`) and `error.type` to see latency and failure rates.
- **Logs:** each failure also arrives as an OTel log record at severity `ERROR`,
  correlated to the trace/span, with the `error.type` — open it from the failing
  span.

---

## Notes

- Dependencies are intentionally minimal: `spring-boot-starter-web` for both,
  plus `opentelemetry-api` on the backend (only to create the three manual
  spans and the `registration.duration` histogram). All exporting — traces,
  metrics, and logs — comes from the javaagent; no OTel SDK wiring in app code.
- Failure logs use SLF4J/Logback (bundled with `spring-boot-starter-web`); the
  agent's log appender exports them over OTLP automatically.
- The Maven wrapper (`.mvn/wrapper/`, `mvnw`, `mvnw.cmd`) is committed so the app
  builds without a system Maven.
- Ports are fixed by the workshop so all languages can run at once:
  frontend **3300**, backend **3301**.
