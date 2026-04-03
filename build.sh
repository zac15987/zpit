#!/usr/bin/env bash
set -e

echo "=== git fetch ==="
git fetch

echo "=== git pull ==="
git pull

echo "=== go build . ==="
go build .

echo "=== go install . ==="
go install .

echo "=== Done ==="
