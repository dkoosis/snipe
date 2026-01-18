#!/bin/bash
# Periodic maintenance for Codex sandbox
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

echo "=== snipe Maintenance ==="

# Activate environment
source "$REPO_ROOT/.codex/activate.sh"

# Clean old caches
echo "Cleaning old caches..."
go clean -cache -testcache 2>/dev/null || true

# Verify modules (non-mutating; avoids go.mod/go.sum churn)
echo "Verifying modules..."
go mod download
go mod verify

# Rebuild snipe binary with low peak RAM settings
echo "Rebuilding snipe (low peak RAM)..."
export GOMAXPROCS=2
go build -p 1 -o "$REPO_ROOT/bin/snipe" .

# Index only if missing (avoid heavy work that can crash constrained sandboxes)
if [[ -f "$REPO_ROOT/.snipe/index.db" ]]; then
    echo "Index exists; skipping reindex"
else
    echo "Building snipe index..."
    "$REPO_ROOT/bin/snipe" index
fi

# Verify binary
echo "Verifying snipe..."
"$REPO_ROOT/bin/snipe" version

echo ""
echo "=== Maintenance Complete ==="

