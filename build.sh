#!/usr/bin/env bash
set -e

echo "=== git fetch ==="
git fetch -v

echo "=== git pull ==="
git pull -v

GIT_VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")
echo "=== version: ${GIT_VERSION} ==="

echo "=== go build . ==="
go build -v -ldflags "-X main.version=${GIT_VERSION}" .

echo "=== go install . ==="
go install -v -ldflags "-X main.version=${GIT_VERSION}" .

echo "=== Done ==="
