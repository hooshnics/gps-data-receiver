#!/bin/bash

# GPS Data Receiver Load Testing Script
# Generates sustained HTTP load (default 10K req/s) against the GPS receiver.

set -e

# Configuration
TARGET_URL="${TARGET_URL:-https://api.hooshnics.com/api/gps/reports}"
DURATION="${DURATION:-30s}"
WARMUP="${WARMUP:-5s}"
RATE="${RATE:-10000}"   # requests per second
WORKERS="${WORKERS:-200}"
TIMEOUT="${TIMEOUT:-5s}"
LOADTEST_BIN="${LOADTEST_BIN:-./bin/loadtest}"

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${GREEN}GPS Data Receiver Load Test${NC}"
echo "================================"
echo "Target URL: $TARGET_URL"
echo "Duration: $DURATION"
echo "Warmup: $WARMUP"
echo "Target Rate: $RATE req/s"
echo "Workers: $WORKERS"
echo ""

# Prefer the built-in Go load tester (best for 10K+ req/s)
if [ -x "$LOADTEST_BIN" ]; then
    echo -e "${GREEN}Using built-in loadtest binary${NC}"
    echo ""
    exec "$LOADTEST_BIN" \
        -url="$TARGET_URL" \
        -duration="$DURATION" \
        -warmup="$WARMUP" \
        -rate="$RATE" \
        -workers="$WORKERS" \
        -timeout="$TIMEOUT"
fi

if command -v go &> /dev/null && [ -f "./cmd/loadtest/main.go" ]; then
    echo -e "${GREEN}Using 'go run' loadtest (build with: make build-loadtest)${NC}"
    echo ""
    exec go run ./cmd/loadtest/main.go \
        -url="$TARGET_URL" \
        -duration="$DURATION" \
        -warmup="$WARMUP" \
        -rate="$RATE" \
        -workers="$WORKERS" \
        -timeout="$TIMEOUT"
fi

# Sample GPS data payload
PAYLOAD='{"device_id":"GPS001","lat":37.7749,"lon":-122.4194,"timestamp":1234567890,"speed":45.5,"altitude":100,"heading":180}'

# Check if hey is installed
if command -v hey &> /dev/null; then
    echo -e "${GREEN}Using 'hey' for load testing${NC}"
    echo ""
    
    hey -z "$DURATION" \
        -q "$RATE" \
        -c "$WORKERS" \
        -m POST \
        -H "Content-Type: application/json" \
        -d "$PAYLOAD" \
        "$TARGET_URL"
    
    exit 0
fi

# Check if vegeta is installed
if command -v vegeta &> /dev/null; then
    echo -e "${GREEN}Using 'vegeta' for load testing${NC}"
    echo ""
    
    echo "POST $TARGET_URL" > /tmp/vegeta_targets.txt
    echo "Content-Type: application/json" >> /tmp/vegeta_targets.txt
    echo "" >> /tmp/vegeta_targets.txt
    echo "$PAYLOAD" >> /tmp/vegeta_targets.txt
    
    vegeta attack \
        -targets=/tmp/vegeta_targets.txt \
        -rate="$RATE" \
        -duration="$DURATION" \
        -workers="$WORKERS" \
        | vegeta report -type=text
    
    rm /tmp/vegeta_targets.txt
    exit 0
fi

# Check if ab (Apache Bench) is installed
if command -v ab &> /dev/null; then
    echo -e "${YELLOW}Using 'ab' for load testing (less accurate for high throughput)${NC}"
    echo ""
    
    # Calculate total requests
    DURATION_SECS=$(echo "$DURATION" | sed 's/s//')
    TOTAL_REQUESTS=$((RATE * DURATION_SECS))
    
    echo "$PAYLOAD" > /tmp/ab_payload.json
    
    ab -n "$TOTAL_REQUESTS" \
       -c "$WORKERS" \
       -p /tmp/ab_payload.json \
       -T "application/json" \
       "$TARGET_URL"
    
    rm /tmp/ab_payload.json
    exit 0
fi

# If no tool is found, provide installation instructions
echo -e "${RED}No load testing tool found!${NC}"
echo ""
echo "Recommended: build the built-in load tester"
echo "  make build-loadtest"
echo "  make load-test"
echo ""
echo "Or install one of the following:"
echo ""
echo "1. hey:"
echo "   macOS: brew install hey"
echo "   Linux: go install github.com/rakyll/hey@latest"
echo ""
echo "2. vegeta:"
echo "   macOS: brew install vegeta"
echo "   Linux: go install github.com/tsenart/vegeta@latest"
echo ""

exit 1
