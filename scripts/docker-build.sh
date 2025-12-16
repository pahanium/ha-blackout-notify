#!/bin/bash
# Script for building Docker image locally

set -e

cd "$(dirname "$0")/.."

ADDON_NAME="blackout-notify"
VERSION=$(grep "version:" blackout-notify/config.yaml | awk '{print $2}' | tr -d '"')
ARCH=${1:-amd64}

echo "=== Building $ADDON_NAME v$VERSION for $ARCH ==="

cd blackout-notify

docker build \
    --build-arg BUILD_FROM=ghcr.io/home-assistant/${ARCH}-base:3.18 \
    -t local/$ADDON_NAME:$VERSION \
    -t local/$ADDON_NAME:latest \
    .

echo "=== Build successful! ==="
echo "Image: local/$ADDON_NAME:$VERSION"
echo ""
echo "To run:"
echo "  docker run --rm -e TELEGRAM_TOKEN=xxx -e HA_TOKEN=xxx -e HA_API_URL=xxx local/$ADDON_NAME:latest"
