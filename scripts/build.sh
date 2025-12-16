#!/bin/bash
# Script for local Go build and test without Docker

set -e

cd "$(dirname "$0")/../blackout-notify/src"

echo "=== Downloading dependencies ==="
go mod tidy

echo "=== Running tests ==="
go test -v ./...

echo "=== Building ==="
CGO_ENABLED=0 go build -ldflags="-w -s" -o ../bin/blackout-notify ./cmd/bot

echo "=== Build successful! ==="
echo "Binary: blackout-notify/bin/blackout-notify"
echo ""
echo "To run locally:"
echo "  export TELEGRAM_TOKEN=your_token"
echo "  export HA_API_URL=http://your-ha:8123/api"
echo "  export HA_TOKEN=your_ha_token"
echo "  ./blackout-notify/bin/blackout-notify"
