#!/bin/bash
# Run JMeter load tests for the GPS Data Receiver.
#
# Usage:
#   ./jmeter/scripts/run-jmeter.sh ingest          # generic JSON throughput test
#   ./jmeter/scripts/run-jmeter.sh hooshnic        # Hooshnic device CSV payloads
#   ./jmeter/scripts/run-jmeter.sh hooshnic-batch  # multi-record payloads from jmeter/data/
#   ./jmeter/scripts/run-jmeter.sh read            # read/query API endpoints
#   ./jmeter/scripts/run-jmeter.sh ingest --gui    # open test plan in JMeter GUI
#
# Environment variables:
#   HOST          Server hostname (default: localhost)
#   PORT          Server port (default: 8080)
#   PROTOCOL      http or https (default: http)
#   THREADS       Concurrent threads (default varies by plan)
#   RAMP_UP       Ramp-up seconds (default: 30)
#   DURATION      Test duration in seconds (default: 60)
#   RATE          Target requests per second (ingest plans only)
#   QUERY_DATE    Date for read API tests (default: 2026-02-24)
#   QUERY_IMEI    IMEI for read API tests (default: 861826074262144)
#   PAYLOAD_FILE  Fixed payload file in jmeter/data/ (hooshnic-batch only; default rotates CSV)
#   PAYLOAD_CSV   Payload rotation CSV (default: hooshnic-payload-files.csv)
#   JMETER_BIN    Path to jmeter executable (auto-detected)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
JMETER_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
PROJECT_ROOT="$(cd "$JMETER_DIR/.." && pwd)"
PLANS_DIR="$JMETER_DIR/plans"
DATA_DIR="$JMETER_DIR/data"
RESULTS_DIR="$JMETER_DIR/results"

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

SCENARIO="${1:-ingest}"
GUI_MODE=false
if [[ "${2:-}" == "--gui" ]]; then
    GUI_MODE=true
fi

HOST="${HOST:-localhost}"
PORT="${PORT:-8080}"
PROTOCOL="${PROTOCOL:-http}"
RAMP_UP="${RAMP_UP:-30}"
DURATION="${DURATION:-60}"
RATE="${RATE:-1000}"
QUERY_DATE="${QUERY_DATE:-2026-02-24}"
QUERY_IMEI="${QUERY_IMEI:-861826074262144}"
PAYLOAD_FILE="${PAYLOAD_FILE:-}"
PAYLOAD_CSV="${PAYLOAD_CSV:-hooshnic-payload-files.csv}"

find_jmeter() {
    if [[ -n "${JMETER_BIN:-}" && -x "$JMETER_BIN" ]]; then
        echo "$JMETER_BIN"
        return
    fi
    if command -v jmeter >/dev/null 2>&1; then
        command -v jmeter
        return
    fi
    for candidate in \
        /opt/homebrew/opt/jmeter/libexec/bin/jmeter \
        /usr/local/opt/jmeter/libexec/bin/jmeter \
        "$HOME/apache-jmeter-"*/bin/jmeter; do
        if [[ -x "$candidate" ]]; then
            echo "$candidate"
            return
        fi
    done
    return 1
}

case "$SCENARIO" in
    ingest)
        PLAN="$PLANS_DIR/gps-ingest-load.jmx"
        THREADS="${THREADS:-100}"
        TARGET_RPM=$((RATE * 60))
        ;;
    hooshnic)
        PLAN="$PLANS_DIR/gps-ingest-hooshnic.jmx"
        THREADS="${THREADS:-50}"
        TARGET_RPM=$((RATE * 60))
        ;;
    hooshnic-batch)
        PLAN="$PLANS_DIR/gps-ingest-hooshnic-batch.jmx"
        THREADS="${THREADS:-50}"
        TARGET_RPM=$((RATE * 60))
        ;;
    read)
        PLAN="$PLANS_DIR/gps-read-apis.jmx"
        THREADS="${THREADS:-10}"
        TARGET_RPM=0
        ;;
    *)
        echo -e "${RED}Unknown scenario: $SCENARIO${NC}"
        echo "Valid scenarios: ingest, hooshnic, hooshnic-batch, read"
        exit 1
        ;;
esac

if [[ ! -f "$PLAN" ]]; then
    echo -e "${RED}Test plan not found: $PLAN${NC}"
    exit 1
fi

JMETER="$(find_jmeter)" || {
    echo -e "${RED}JMeter not found.${NC}"
    echo ""
    echo "Install JMeter:"
    echo "  macOS:  brew install jmeter"
    echo "  Linux:  apt install jmeter  OR  download from https://jmeter.apache.org/download_jmeter.cgi"
    echo ""
    echo "Then re-run:"
    echo "  make jmeter-$SCENARIO"
    exit 1
}

TIMESTAMP="$(date +%Y%m%d-%H%M%S)"
RUN_DIR="$RESULTS_DIR/${SCENARIO}-${TIMESTAMP}"
mkdir -p "$RUN_DIR"

resolve_plan() {
    local source="$1"
    local dest="$2"
    local rpm="$3"
    cp "$source" "$dest"
    if [[ "$rpm" -gt 0 ]]; then
        if [[ "$(uname)" == "Darwin" ]]; then
            sed -i '' "s/<value>999999<\\/value>/<value>${rpm}<\\/value>/" "$dest"
        else
            sed -i "s/<value>999999<\\/value>/<value>${rpm}<\\/value>/" "$dest"
        fi
    fi
}

RESOLVED_PLAN="$RUN_DIR/plan-resolved.jmx"
resolve_plan "$PLAN" "$RESOLVED_PLAN" "$TARGET_RPM"
PLAN="$RESOLVED_PLAN"

echo -e "${GREEN}GPS Data Receiver — JMeter Load Test${NC}"
echo "======================================"
echo "Scenario:    $SCENARIO"
echo "Test plan:   $PLAN"
echo "Target:      ${PROTOCOL}://${HOST}:${PORT}"
echo "Threads:     $THREADS"
echo "Ramp-up:     ${RAMP_UP}s"
echo "Duration:    ${DURATION}s"
if [[ "$SCENARIO" != "read" ]]; then
    echo "Target rate: ${RATE} req/s (${TARGET_RPM} req/min)"
fi
if [[ "$SCENARIO" == "read" ]]; then
    echo "Query date:  $QUERY_DATE"
    echo "Query IMEI:  $QUERY_IMEI"
fi
if [[ "$SCENARIO" == "hooshnic-batch" ]]; then
    if [[ -n "$PAYLOAD_FILE" ]]; then
        echo "Payload:     $PAYLOAD_FILE (fixed)"
    else
        echo "Payload:     rotate via $PAYLOAD_CSV"
    fi
fi
echo "Results:     $RUN_DIR"
echo ""

JMETER_PROPS=(
    -JHOST="$HOST"
    -JPORT="$PORT"
    -JPROTOCOL="$PROTOCOL"
    -JTHREADS="$THREADS"
    -JRAMP_UP="$RAMP_UP"
    -JDURATION="$DURATION"
    -JDATA_DIR="$DATA_DIR"
)

if [[ "$SCENARIO" != "read" ]]; then
    JMETER_PROPS+=(-JTARGET_RPM="$TARGET_RPM")
fi

if [[ "$SCENARIO" == "read" ]]; then
    JMETER_PROPS+=(-JQUERY_DATE="$QUERY_DATE" -JQUERY_IMEI="$QUERY_IMEI")
fi

if [[ "$SCENARIO" == "hooshnic-batch" ]]; then
    JMETER_PROPS+=(-JPAYLOAD_CSV="$PAYLOAD_CSV")
    if [[ -n "$PAYLOAD_FILE" ]]; then
        JMETER_PROPS+=(-JPAYLOAD_FILE="$PAYLOAD_FILE")
    fi
fi

if $GUI_MODE; then
    echo -e "${YELLOW}Opening JMeter GUI...${NC}"
    exec "$JMETER" -t "$PLAN" "${JMETER_PROPS[@]}"
fi

JTL_FILE="$RUN_DIR/results.jtl"
REPORT_DIR="$RUN_DIR/report"

echo -e "${GREEN}Running non-GUI test...${NC}"
"$JMETER" -n \
    -t "$PLAN" \
    -l "$JTL_FILE" \
    -j "$RUN_DIR/jmeter.log" \
    -e -o "$REPORT_DIR" \
    "${JMETER_PROPS[@]}"

echo ""
echo -e "${GREEN}Test complete.${NC}"
echo "  JTL results:  $JTL_FILE"
echo "  HTML report:  $REPORT_DIR/index.html"
echo "  JMeter log:   $RUN_DIR/jmeter.log"
echo ""
echo "Open the HTML report:"
echo "  open $REPORT_DIR/index.html"
