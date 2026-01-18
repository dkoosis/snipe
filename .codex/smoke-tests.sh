#!/bin/bash
# Ultra-fast smoke tests (<10s)
set -e

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

# Activate environment
source "$REPO_ROOT/.codex/activate.sh"

echo "=== snipe Smoke Tests ==="

# Quick syntax check
echo "Running go vet..."
go vet ./...

# Fast tests only (skip long-running tests)
echo "Running fast tests..."
go test -short -count=1 ./internal/query/... ./internal/output/... ./internal/config/...

# Build check
echo "Building snipe..."
go build -o /dev/null .

echo ""
echo "=== Smoke Tests Passed ==="
