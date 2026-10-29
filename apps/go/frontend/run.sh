#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
# repo-root/.env (service is at apps/go/frontend)
source ../../../scripts/load-env.sh
export OTEL_SERVICE_NAME=registration-web-go
export PORT="${PORT:-3200}"
export BACKEND_URL="${BACKEND_URL:-http://localhost:3201}"
exec go run .
