#!/bin/bash
# Periodic maintenance for Codex sandbox
set -e

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

echo "=== snipe Maintenance ==="

# Activate environment
source "$REPO_ROOT/.codex/activate.sh"

# Clean old caches
echo "Cleaning old caches..."
go clean -cache -testcache 2>/dev/null || true

# Ensure go.mod is tidy
echo "Tidying go.mod..."
go mod tidy

# Rebuild snipe binary
echo "Rebuilding snipe..."
go build -o "$REPO_ROOT/bin/snipe" .

# Reindex if .snipe exists
if [[ -d "$REPO_ROOT/.snipe" ]]; then
    echo "Reindexing snipe..."
    "$REPO_ROOT/bin/snipe" index
fi

# Verify binary
echo "Verifying snipe..."
"$REPO_ROOT/bin/snipe" version

echo ""
echo "=== Maintenance Complete ==="
