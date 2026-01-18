#!/bin/bash
# Activate snipe development environment
# Usage: source .codex/activate.sh

# Find repo root
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

# Set up isolated caches in .codex/cache
export GOCACHE="$REPO_ROOT/.codex/cache/go-build"
export GOMODCACHE="$REPO_ROOT/.codex/cache/mod"
export GOLANGCI_LINT_CACHE="$REPO_ROOT/.codex/cache/golangci-lint"

# Ensure cache directories exist
mkdir -p "$GOCACHE" "$GOMODCACHE" "$GOLANGCI_LINT_CACHE"

# Detect platform
PLATFORM="$(uname -s)-$(uname -m)"
case "$PLATFORM" in
    Linux-x86_64)  BINDIR="$REPO_ROOT/.codex/bin/linux-amd64" ;;
    Darwin-x86_64) BINDIR="$REPO_ROOT/.codex/bin/darwin-amd64" ;;
    Darwin-arm64)  BINDIR="$REPO_ROOT/.codex/bin/darwin-arm64" ;;
    *)             BINDIR="" ;;
esac

# Add paths to PATH
export PATH="$REPO_ROOT/bin:$BINDIR:$PATH"

# Link prebuilt binaries if available and bin directory doesn't have them
if [[ -d "$BINDIR" ]]; then
    mkdir -p "$REPO_ROOT/bin"
    for tool in golangci-lint mage rg; do
        if [[ -x "$BINDIR/$tool" && ! -e "$REPO_ROOT/bin/$tool" ]]; then
            ln -sf "$BINDIR/$tool" "$REPO_ROOT/bin/$tool"
        fi
    done
fi

# Performance tuning
export GOMAXPROCS=${GOMAXPROCS:-$(nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 4)}
export GOTOOLCHAIN=auto

# Increase file descriptor limit if possible
ulimit -n 65536 2>/dev/null || true

echo "snipe environment activated"
echo "  GOCACHE=$GOCACHE"
echo "  GOMODCACHE=$GOMODCACHE"
echo "  PATH includes: $REPO_ROOT/bin"

