#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
set -a; source ../../../.env; set +a      # repo-root/.env (service is at apps/nodejs/backend)
export OTEL_SERVICE_NAME=registration-api-node
export PORT=3001
exec node -r ./tracing.js server.js
