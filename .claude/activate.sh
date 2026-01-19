#!/bin/bash
# Activate snipe development environment for Claude sandbox
# Usage: source .claude/activate.sh

# Find repo root
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

# Set up isolated caches in .claude/cache
export GOCACHE="$REPO_ROOT/.claude/cache/go-build"
export GOMODCACHE="$REPO_ROOT/.claude/cache/mod"
export GOLANGCI_LINT_CACHE="$REPO_ROOT/.claude/cache/golangci-lint"

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

# Add paths to PATH (share binaries from .codex)
# Note: setup.sh handles symlinking binaries to ./bin
export PATH="$REPO_ROOT/bin:$BINDIR:$PATH"

# Performance tuning
export GOMAXPROCS=${GOMAXPROCS:-$(nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 4)}
export GOTOOLCHAIN=auto

# Increase file descriptor limit if possible
ulimit -n 65536 2>/dev/null || true

# Output status unless quiet mode is enabled
if [[ -z "${CLAUDE_ACTIVATE_QUIET:-}" ]]; then
    echo "snipe environment activated for Claude"
    echo "  GOCACHE=$GOCACHE"
    echo "  GOMODCACHE=$GOMODCACHE"
    echo "  PATH includes: $REPO_ROOT/bin"
    echo "  Sharing binaries from: $BINDIR"
fi
