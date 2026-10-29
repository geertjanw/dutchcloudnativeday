#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
# repo-root/.env (service is at apps/go/backend)
source ../../../scripts/load-env.sh
export OTEL_SERVICE_NAME=registration-api-go
export PORT="${PORT:-3201}"
exec go run .
