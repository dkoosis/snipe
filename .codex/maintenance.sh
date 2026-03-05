#!/usr/bin/env bash
# Codex cached container refresh for snipe
# Runs when a cached container is reused for a new task.
# Keep lightweight — setup.sh already installed tools.
set -euo pipefail

REPO_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_DIR"

echo "=== snipe maintenance ==="

# Refresh go modules (deps may have changed)
go mod download
echo "  go modules refreshed"

# Rebuild snipe index (source may have changed)
if command -v snipe >/dev/null 2>&1; then
  snipe index --embed-mode=off --enrich=false 2>/dev/null && echo "  snipe index rebuilt" || echo "  snipe index skipped"
fi

# Quick verify — fail fast if tools disappeared
MISSING=0
for tool in go snipe mage golangci-lint govulncheck gofumpt goimports; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    printf "  MISSING  %s\n" "$tool"
    MISSING=$((MISSING + 1))
  fi
done

if [ "$MISSING" -gt 0 ]; then
  echo "WARNING: $MISSING tool(s) missing — container cache may be stale, re-run setup"
  exit 1
fi

echo "=== maintenance complete ==="
