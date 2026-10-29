# Shared env loader for every app's run.sh — source this, do not execute it.
#
#   source ../../../scripts/load-env.sh
#
# It loads the repo-root .env (Dash0 credentials + OTLP settings) and then
# sanity-checks the OTLP endpoint against the protocol, because the single most
# common workshop mistake is pasting the "OTLP via gRPC" endpoint (port 4317)
# while the protocol is http/protobuf — which the collector rejects with
# HTTP 415. See the repo README for the full explanation.

# Resolve the repo root from this file's own location, independent of the
# caller's working directory.
_repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [ ! -f "$_repo_root/.env" ]; then
  echo "No .env found at $_repo_root/.env" >&2
  echo "Copy the template first:  cp .env.example .env" >&2
  exit 1
fi

set -a
# shellcheck disable=SC1090
source "$_repo_root/.env"
set +a

# http/protobuf speaks HTTP, so it must target the OTLP/HTTP port (4318) or the
# default (443) — never 4317, which is the gRPC port.
if [ "${OTEL_EXPORTER_OTLP_PROTOCOL:-}" = "http/protobuf" ]; then
  case "${OTEL_EXPORTER_OTLP_ENDPOINT:-}" in
    *:4317 | *:4317/*)
      _fixed="${OTEL_EXPORTER_OTLP_ENDPOINT/:4317/:4318}"
      echo "WARN: OTEL_EXPORTER_OTLP_ENDPOINT uses :4317 (the gRPC port) but" >&2
      echo "      OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf speaks HTTP." >&2
      echo "      Auto-correcting to :4318 for this run. Fix .env to silence this." >&2
      export OTEL_EXPORTER_OTLP_ENDPOINT="$_fixed"
      unset _fixed
      ;;
  esac
fi

unset _repo_root
