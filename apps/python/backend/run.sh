#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

# Load shared Dash0 / OTLP settings from the repo-root .env.
set -a; source ../../../.env; set +a

# Activate this service's isolated virtualenv.
source venv/bin/activate

# Per-service OpenTelemetry + listen settings.
export OTEL_SERVICE_NAME=registration-api-python
export PORT=3101

exec python app.py
