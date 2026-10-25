#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

# Load the shared root .env (repo-root/.env; this service lives at apps/java/frontend).
set -a; source ../../../.env; set +a

# Java uses the javaagent (no app code), so build the OTLP auth headers here.
export OTEL_EXPORTER_OTLP_HEADERS="Authorization=Bearer ${DASH0_AUTH_TOKEN},Dash0-Dataset=${DASH0_DATASET}"

export OTEL_SERVICE_NAME=registration-web-java
export PORT="${PORT:-3300}"
export BACKEND_URL="${BACKEND_URL:-http://localhost:3301}"

AGENT="../opentelemetry-javaagent.jar"
JAR="target/registration-web-java.jar"

if [ ! -f "$AGENT" ]; then
  echo "OpenTelemetry javaagent not found at $AGENT" >&2
  echo "Run ./download-agent.sh (in apps/java) first." >&2
  exit 1
fi

# Build the jar if it is missing.
if [ ! -f "$JAR" ]; then
  echo "Building $JAR ..."
  ./mvnw -q package -DskipTests
fi

exec java -javaagent:"$AGENT" -Dserver.port="$PORT" -jar "$JAR"
