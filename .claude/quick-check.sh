#!/bin/bash
# Ultra-fast smoke tests for Claude (<10s)
set -e

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

# Activate environment
source "$REPO_ROOT/.claude/activate.sh"

echo "=== snipe Quick Check ==="

# Quick syntax check
echo "Running go vet..."
go vet ./...

# Fast tests only (skip long-running tests)
echo "Running fast tests..."
go test -short -count=1 ./internal/query/... ./internal/output/... ./internal/config/...

# Build check
echo "Building snipe..."
go build -o /dev/null .

# Verify snipe index exists
if [[ -f "$REPO_ROOT/.snipe/index.db" ]]; then
    echo "Snipe index: OK"
else
    echo "Warning: Snipe index not found at .snipe/index.db"
fi

echo ""
echo "=== Quick Check Passed ==="
