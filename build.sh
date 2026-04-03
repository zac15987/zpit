#!/usr/bin/env bash
set -e

echo "=== go build . ==="
go build .

echo "=== go install . ==="
go install .

echo "=== Done ==="
