#!/bin/bash
# Smoke-test Hooshnic payload samples against a running GPS receiver.

set -euo pipefail

TARGET_URL="${TARGET_URL:-http://localhost:8080/api/gps/reports}"
DATA_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../jmeter/data" && pwd)"

GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m'

post_payload() {
    local name="$1"
    local file="$2"

    echo -n "Testing ${name}... "
    response="$(curl -sS -w "\n%{http_code}" -X POST "$TARGET_URL" \
        -H "Content-Type: application/json" \
        --data-binary "@${file}")"
    body="$(echo "$response" | sed '$d')"
    code="$(echo "$response" | tail -n 1)"

    if [[ "$code" == "200" && "$body" == *"queued"* ]]; then
        echo -e "${GREEN}OK${NC} (HTTP $code)"
        return 0
    fi

    echo -e "${RED}FAIL${NC} (HTTP $code)"
    echo "  Response: $body"
    return 1
}

echo "Hooshnic payload smoke test"
echo "Target: $TARGET_URL"
echo ""

failed=0
post_payload "3-record batch with empty object" "${DATA_DIR}/hooshnic-batch-3records.json" || failed=$((failed + 1))
post_payload "single record with trailing dots" "${DATA_DIR}/hooshnic-single-trailing-dots.body" || failed=$((failed + 1))

echo ""
if [[ "$failed" -eq 0 ]]; then
    echo -e "${GREEN}All smoke tests passed.${NC}"
    exit 0
fi

echo -e "${RED}${failed} smoke test(s) failed.${NC}"
exit 1
