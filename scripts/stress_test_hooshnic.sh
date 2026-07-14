#!/bin/bash
# Progressive stress test for Hooshnic batch payloads using the Go load tester.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
LOADTEST_BIN="${LOADTEST_BIN:-$PROJECT_ROOT/bin/loadtest}"
PAYLOAD_FILE="${PAYLOAD_FILE:-$PROJECT_ROOT/jmeter/data/hooshnic-batch-3records.json}"
TARGET_URL="${TARGET_URL:-http://localhost:8080/api/gps/reports}"
DURATION="${DURATION:-30s}"
WARMUP="${WARMUP:-5s}"
WORKERS="${WORKERS:-200}"
TIMEOUT="${TIMEOUT:-5s}"
RATES="${RATES:-500 1000 2000 5000 8000 10000}"
PAUSE_SECS="${PAUSE_SECS:-10}"

if [[ ! -x "$LOADTEST_BIN" ]]; then
    echo "Building loadtest binary..."
    make -C "$PROJECT_ROOT" build-loadtest
fi

echo "Hooshnic progressive stress test"
echo "================================"
echo "Target URL:   $TARGET_URL"
echo "Payload file: $PAYLOAD_FILE"
echo "Rates:        $RATES"
echo "Duration:     $DURATION per step"
echo ""

for rate in $RATES; do
    echo ""
    echo "=== Step: ${rate} req/s ==="
    "$LOADTEST_BIN" \
        -url="$TARGET_URL" \
        -payload="$PAYLOAD_FILE" \
        -duration="$DURATION" \
        -warmup="$WARMUP" \
        -rate="$rate" \
        -workers="$WORKERS" \
        -timeout="$TIMEOUT"
    echo "Cooling down for ${PAUSE_SECS}s..."
    sleep "$PAUSE_SECS"
done

echo ""
echo "Stress test complete."
