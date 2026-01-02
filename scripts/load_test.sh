#!/bin/bash

# GPS Data Receiver Load Testing Script
# This script performs load testing on the GPS data receiver endpoint

set -e

# Configuration
TARGET_URL="${TARGET_URL:-http://localhost:8080/api/gps/reports}"
DURATION="${DURATION:-60s}"
RATE="${RATE:-1000}"  # requests per second
WORKERS="${WORKERS:-50}"

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${GREEN}GPS Data Receiver Load Test${NC}"
echo "================================"
echo "Target URL: $TARGET_URL"
echo "Duration: $DURATION"
echo "Target Rate: $RATE req/s"
echo "Workers: $WORKERS"
echo ""

# Sample GPS data payload
PAYLOAD='{"device_id":"GPS001","lat":37.7749,"lon":-122.4194,"timestamp":1234567890,"speed":45.5,"altitude":100,"heading":180}'

# Check if hey is installed
if command -v hey &> /dev/null; then
    echo -e "${GREEN}Using 'hey' for load testing${NC}"
    echo ""
    
    hey -z $DURATION \
        -q $RATE \
        -c $WORKERS \
        -m POST \
        -H "Content-Type: application/json" \
        -d "$PAYLOAD" \
        $TARGET_URL
    
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
        -rate=$RATE \
        -duration=$DURATION \
        -workers=$WORKERS \
        | vegeta report -type=text
    
    rm /tmp/vegeta_targets.txt
    exit 0
fi

# Check if ab (Apache Bench) is installed
if command -v ab &> /dev/null; then
    echo -e "${YELLOW}Using 'ab' for load testing (less accurate for high throughput)${NC}"
    echo ""
    
    # Calculate total requests
    DURATION_SECS=$(echo $DURATION | sed 's/s//')
    TOTAL_REQUESTS=$((RATE * DURATION_SECS))
    
    echo "$PAYLOAD" > /tmp/ab_payload.json
    
    ab -n $TOTAL_REQUESTS \
       -c $WORKERS \
       -p /tmp/ab_payload.json \
       -T "application/json" \
       $TARGET_URL
    
    rm /tmp/ab_payload.json
    exit 0
fi

# If no tool is found, provide installation instructions
echo -e "${RED}No load testing tool found!${NC}"
echo ""
echo "Please install one of the following:"
echo ""
echo "1. hey (recommended):"
echo "   macOS: brew install hey"
echo "   Linux: go install github.com/rakyll/hey@latest"
echo ""
echo "2. vegeta:"
echo "   macOS: brew install vegeta"
echo "   Linux: go install github.com/tsenart/vegeta@latest"
echo ""
echo "3. Apache Bench (ab):"
echo "   macOS: comes pre-installed"
echo "   Ubuntu/Debian: sudo apt-get install apache2-utils"
echo ""
echo "Alternative: Use the manual load test below"
echo ""
echo "=== Manual Load Test with curl ==="
echo "Run this in multiple terminals:"
echo ""
echo 'for i in {1..1000}; do'
echo '  curl -X POST http://localhost:8080/api/gps/reports \'
echo '    -H "Content-Type: application/json" \'
echo "    -d '$PAYLOAD' &"
echo 'done'
echo 'wait'

exit 1

