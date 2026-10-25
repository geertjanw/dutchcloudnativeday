#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

# Downloads the latest OpenTelemetry Java agent into apps/java/opentelemetry-javaagent.jar.
# Both services load it via -javaagent in their run.sh.

DEST="opentelemetry-javaagent.jar"
URL="https://github.com/open-telemetry/opentelemetry-java-instrumentation/releases/latest/download/opentelemetry-javaagent.jar"

echo "Downloading OpenTelemetry Java agent -> $DEST"
curl -fL --retry 3 -o "$DEST" "$URL"

echo "Done: $(ls -lh "$DEST" | awk '{print $5}')"
