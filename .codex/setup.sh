#!/usr/bin/env bash
# Codex cloud environment setup for snipe
# Auto-discovered by Codex from .codex/setup.sh on first container creation.
# Cached ~12h; .codex/maintenance.sh refreshes cached containers.
# Exports don't persist into agent phase — use ~/.bashrc or install to PATH.
set -euo pipefail

# Derive repo root from this script's location (.codex/setup.sh → parent)
REPO_DIR="$(cd "$(dirname "$0")/.." && pwd)"
ARCH=$(uname -m)
case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *)
    echo "Unsupported architecture: $ARCH" >&2
    exit 1
    ;;
esac
PREBUILT_DIR="$REPO_DIR/.bin/linux-$ARCH"
INSTALL_DIR="/usr/local/bin"

echo "=== snipe sandbox setup ==="

# --- 1. System aliases ---
# Ubuntu fd-find installs as fdfind; scripts expect fd
if command -v fdfind >/dev/null 2>&1 && ! command -v fd >/dev/null 2>&1; then
  ln -sf "$(command -v fdfind)" "$INSTALL_DIR/fd"
  echo "  aliased fdfind -> fd"
fi

# --- 2. Prebuilt binaries (seconds, not minutes) ---
if [ -d "$PREBUILT_DIR" ]; then
  for tool in "$PREBUILT_DIR"/*; do
    [ -f "$tool" ] || continue
    toolname=$(basename "$tool")
    cp "$tool" "$INSTALL_DIR/$toolname"
    chmod +x "$INSTALL_DIR/$toolname"
    echo "  installed $toolname (prebuilt)"
  done
fi

# --- 3. Go module cache (warm) ---
cd "$REPO_DIR"
go mod download
echo "  go modules downloaded"

# --- 4. Snipe index (warm cache for code navigation) ---
if command -v snipe >/dev/null 2>&1; then
  snipe index --embed-mode=off --enrich=false 2>/dev/null && echo "  snipe index built" || echo "  snipe index skipped"
fi

# --- 5. Verify ---
echo ""
echo "=== tool verification ==="
MISSING=0
for tool in go snipe mage golangci-lint govulncheck gofumpt goimports jq rg fd bat dtree; do
  if command -v "$tool" >/dev/null 2>&1; then
    printf "  ok  %s\n" "$tool"
  else
    printf "  MISSING  %s\n" "$tool"
    MISSING=$((MISSING + 1))
  fi
done

if [ "$MISSING" -gt 0 ]; then
  echo ""
  echo "WARNING: $MISSING required tool(s) missing — mage qa will fail"
  exit 1
fi

echo ""
echo "=== setup complete ==="
