#!/usr/bin/env bash
set -e

echo "=== git fetch ==="
git fetch -v

echo "=== git pull ==="
git pull -v

echo "=== go build . ==="
go build -v .

echo "=== go install . ==="
go install -v .

echo "=== Done ==="
