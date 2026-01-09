#!/bin/bash
# Bash script to test durability of mini-redis
# This script sets 10 keys, then verifies they can be retrieved

echo "=== Mini Redis Durability Test ==="
echo ""

BASE_URL="http://localhost:8080"
KEYS=()

# Step 1: Set 10 keys
echo "Step 1: Setting 10 keys..."
for i in {1..10}; do
    KEY="key$i"
    VALUE="value$i"
    BODY="{\"key\":\"$KEY\",\"value\":\"$VALUE\"}"
    
    if curl -s -X POST "$BASE_URL/set" \
        -H "Content-Type: application/json" \
        -d "$BODY" > /dev/null; then
        echo "  [OK] Set $KEY = $VALUE"
        KEYS+=("$KEY")
    else
        echo "  [FAIL] Failed to set $KEY"
        exit 1
    fi
done

echo ""
echo "Step 2: Verifying keys exist..."

# Step 2: Verify keys exist
ALL_FOUND=true
for KEY in "${KEYS[@]}"; do
    VALUE=$(curl -s "$BASE_URL/get?key=$KEY")
    if [ $? -eq 0 ] && [ -n "$VALUE" ]; then
        echo "  [OK] $KEY = $VALUE"
    else
        echo "  [FAIL] $KEY not found!"
        ALL_FOUND=false
    fi
done

echo ""
if [ "$ALL_FOUND" = true ]; then
    echo "[SUCCESS] All keys verified successfully!"
    echo ""
    echo "Next steps:"
    echo "1. Kill the server (CTRL+C)"
    echo "2. Restart the server: go run ./cmd/server"
    echo "3. Run this script again to verify keys survived the restart"
else
    echo "[FAIL] Some keys are missing!"
    exit 1
fi
