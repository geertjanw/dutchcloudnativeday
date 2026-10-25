#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
# repo-root/.env (service is at apps/go/backend)
set -a; source ../../../.env; set +a
export OTEL_SERVICE_NAME=registration-api-go
export PORT="${PORT:-3201}"
exec go run .
