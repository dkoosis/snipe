#!/bin/bash
# One-time environment setup for Claude sandbox
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

echo "=== snipe Claude Sandbox Setup ==="

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

# Link prebuilt binaries from .codex if available (shared resources)
if [[ -d "$BINDIR" ]]; then
    echo "Linking prebuilt binaries from .codex/bin/..."
    for tool in snipe golangci-lint mage rg govulncheck; do
        if [[ -x "$BINDIR/$tool" ]]; then
            ln -sf "$BINDIR/$tool" "$REPO_ROOT/bin/$tool"
            echo "  Linked: $tool"
        fi
    done
fi

# Set up Claude-specific caches (isolated from .codex)
export GOCACHE="$REPO_ROOT/.claude/cache/go-build"
export GOMODCACHE="$REPO_ROOT/.claude/cache/mod"
export GOLANGCI_LINT_CACHE="$REPO_ROOT/.claude/cache/golangci-lint"
mkdir -p "$GOCACHE" "$GOMODCACHE" "$GOLANGCI_LINT_CACHE"

export PATH="$REPO_ROOT/bin:$BINDIR:$PATH"
export GOTOOLCHAIN=auto

# Check/install Go (if not present)
if ! command -v go &>/dev/null; then
    echo "Go not found, installing..."
    GO_VERSION="1.25.1"
    GO_TARBALL="go${GO_VERSION}.linux-amd64.tar.gz"
    GO_URL="https://go.dev/dl/${GO_TARBALL}"

    # Download to temp file for verification
    TMPDIR=$(mktemp -d)
    trap 'rm -rf "$TMPDIR"' EXIT

    echo "  Downloading ${GO_TARBALL}..."
    curl -fsSL "$GO_URL" -o "$TMPDIR/${GO_TARBALL}"

    # Note: For production, add checksum verification here
    # GO_SHA256="expected_checksum_here"
    # echo "${GO_SHA256}  $TMPDIR/${GO_TARBALL}" | sha256sum -c -

    echo "  Extracting to /usr/local..."
    tar -C /usr/local -xzf "$TMPDIR/${GO_TARBALL}"
    export PATH="/usr/local/go/bin:$PATH"
fi

echo "Go: $(go version)"

# Install golangci-lint if not linked
if ! command -v golangci-lint &>/dev/null; then
    echo "Installing golangci-lint..."
    GOLANGCI_VERSION="1.64.2"
    GOLANGCI_TARBALL="golangci-lint-${GOLANGCI_VERSION}-linux-amd64.tar.gz"
    GOLANGCI_URL="https://github.com/golangci/golangci-lint/releases/download/v${GOLANGCI_VERSION}/${GOLANGCI_TARBALL}"

    TMPDIR=$(mktemp -d)
    trap 'rm -rf "$TMPDIR"' EXIT

    echo "  Downloading golangci-lint v${GOLANGCI_VERSION}..."
    curl -fsSL "$GOLANGCI_URL" -o "$TMPDIR/${GOLANGCI_TARBALL}"

    echo "  Extracting..."
    tar -C "$TMPDIR" -xzf "$TMPDIR/${GOLANGCI_TARBALL}"
    cp "$TMPDIR/golangci-lint-${GOLANGCI_VERSION}-linux-amd64/golangci-lint" "$REPO_ROOT/bin/"
    chmod +x "$REPO_ROOT/bin/golangci-lint"
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
        sudo apt-get update && sudo apt-get install -y ripgrep || echo "  Note: ripgrep install requires sudo permissions"
    fi
fi

if command -v rg &>/dev/null; then
    echo "ripgrep: $(rg --version | head -1)"
else
    echo "Warning: ripgrep not available (snipe search may not work)"
fi

echo ""
echo "Downloading Go modules..."
go mod download
go mod verify

# Build snipe if not linked
if [[ ! -x "$REPO_ROOT/bin/snipe" ]]; then
    echo "Building snipe (optimized for sandbox)..."
    # -p 1 reduces parallel compile RAM spikes in constrained sandboxes
    go build -p 1 -o "$REPO_ROOT/bin/snipe" .
fi

echo ""
echo "Verifying snipe..."
"$REPO_ROOT/bin/snipe" version

# Check for pre-built index (committed to repo)
if [[ -f "$REPO_ROOT/.snipe/index.db" ]]; then
    echo "Snipe index: .snipe/index.db (pre-built)"
else
    echo "Warning: Snipe index not found. It should be committed in the repo."
    echo "To rebuild: snipe index"
fi

echo ""
echo "=== Setup Complete ==="
echo ""
echo "To validate: bash .claude/quick-check.sh"
echo "To activate: source .claude/activate.sh"
