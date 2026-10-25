#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
set -a; source ../../../.env; set +a      # repo-root/.env (service is at apps/nodejs/frontend)
export OTEL_SERVICE_NAME=registration-web-node
export PORT=3000
export BACKEND_URL=http://localhost:3001
exec node -r ./tracing.js server.js
