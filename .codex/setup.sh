#!/bin/bash
# One-time environment setup for Codex sandbox
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

echo "=== snipe Codex Environment Setup ==="

# Detect platform
PLATFORM="$(uname -s)-$(uname -m)"
case "$PLATFORM" in
    Linux-x86_64)  BINDIR="$REPO_ROOT/.codex/bin/linux-amd64" ;;
    Darwin-x86_64) BINDIR="$REPO_ROOT/.codex/bin/darwin-amd64" ;;
    Darwin-arm64)  BINDIR="$REPO_ROOT/.codex/bin/darwin-arm64" ;;
    *)             BINDIR="" ;;
esac

echo "Platform: $PLATFORM"

# Create bin directory
mkdir -p "$REPO_ROOT/bin"

# Link prebuilt binaries if available
if [[ -d "$BINDIR" ]]; then
    echo "Linking prebuilt binaries from $BINDIR..."
    for tool in snipe golangci-lint mage rg govulncheck; do
        if [[ -x "$BINDIR/$tool" ]]; then
            ln -sf "$BINDIR/$tool" "$REPO_ROOT/bin/$tool"
            echo "  Linked: $tool"
        fi
    done
fi

# Set up caches (isolated)
export GOCACHE="$REPO_ROOT/.codex/cache/go-build"
export GOMODCACHE="$REPO_ROOT/.codex/cache/mod"
export GOLANGCI_LINT_CACHE="$REPO_ROOT/.codex/cache/golangci-lint"
mkdir -p "$GOCACHE" "$GOMODCACHE" "$GOLANGCI_LINT_CACHE"

export PATH="$REPO_ROOT/bin:$BINDIR:$PATH"
export GOTOOLCHAIN=auto

# Check/install Go (if not present)
if ! command -v go &>/dev/null; then
    echo "Go not found, installing..."
    GO_VERSION="1.24.3"
    curl -sL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" | tar -C /usr/local -xzf -
    export PATH="/usr/local/go/bin:$PATH"
fi

echo "Go: $(go version)"

# Install golangci-lint if not linked
if ! command -v golangci-lint &>/dev/null; then
    echo "Installing golangci-lint..."
    curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b "$REPO_ROOT/bin" v1.64.2
fi
echo "golangci-lint: $(golangci-lint --version 2>&1 | head -1)"

# Install mage if not linked
if ! command -v mage &>/dev/null; then
    echo "Installing mage..."
    go install github.com/magefile/mage@latest
    cp "$(go env GOPATH)/bin/mage" "$REPO_ROOT/bin/"
fi
echo "mage: $(mage -version 2>&1 | head -1)"

# Check for ripgrep (required for snipe search)
if ! command -v rg &>/dev/null; then
    echo "Installing ripgrep..."
    if command -v apt-get &>/dev/null; then
        apt-get update && apt-get install -y ripgrep || true
    fi
fi

if command -v rg &>/dev/null; then
    echo "ripgrep: $(rg --version | head -1)"
else
    echo "Warning: ripgrep not available (snipe search may not work)"
fi

# Install govulncheck if not linked (required for mage qa)
if ! command -v govulncheck &>/dev/null; then
    echo "Installing govulncheck..."
    go install golang.org/x/vuln/cmd/govulncheck@latest
    cp "$(go env GOPATH)/bin/govulncheck" "$REPO_ROOT/bin/" 2>/dev/null || true
fi
if command -v govulncheck &>/dev/null; then
    echo "govulncheck: $(govulncheck -version 2>&1 | head -1)"
fi

echo ""
echo "Downloading Go modules..."
go mod download
go mod verify

# Build snipe if not linked
if [[ ! -x "$REPO_ROOT/bin/snipe" ]]; then
    echo "Building snipe (low peak RAM)..."
    # -p 1 reduces parallel compile RAM spikes in constrained sandboxes
    go build -p 1 -o "$REPO_ROOT/bin/snipe" .
fi

echo ""
echo "Verifying snipe..."
"$REPO_ROOT/bin/snipe" version

# Build the codebase index if missing
if [[ -f "$REPO_ROOT/.snipe/index.db" ]]; then
    echo "Snipe index found: .snipe/index.db"
else
    echo "Building snipe index..."
    "$REPO_ROOT/bin/snipe" index
fi

echo ""
echo "=== Setup Complete ==="
echo ""
echo "To validate: bash .codex/validate.sh"
echo "To activate: source .codex/activate.sh"

