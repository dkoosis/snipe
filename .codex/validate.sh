#!/bin/bash
# Full QA validation for Codex
set -e

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

# Activate environment
source "$REPO_ROOT/.codex/activate.sh"

echo "=== snipe Validation ==="

# Check required tools
for tool in go golangci-lint mage; do
    if ! command -v $tool &>/dev/null; then
        echo "Error: $tool not found. Run: bash .codex/setup.sh"
        exit 1
    fi
done

# Run mage qa (lint + tests + build)
echo ""
echo "Running mage qa..."
mage qa

# Verify snipe binary works
if [[ -f "$REPO_ROOT/bin/snipe" ]]; then
    echo ""
    echo "Verifying snipe binary..."
    "$REPO_ROOT/bin/snipe" version
fi

echo ""
echo "=== Validation Passed ==="
