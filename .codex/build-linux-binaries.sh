#!/bin/bash
# Build/download Linux binaries for Codex sandbox
set -e

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

BINDIR="$REPO_ROOT/.codex/bin/linux-amd64"
mkdir -p "$BINDIR"

echo "=== Building Linux Binaries ==="

# Build snipe for Linux
echo "Building snipe for linux-amd64..."
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o "$BINDIR/snipe" .
echo "  Built: $BINDIR/snipe"

# Download golangci-lint
GOLANGCI_VERSION="1.64.2"
if [[ ! -f "$BINDIR/golangci-lint" ]]; then
    echo "Downloading golangci-lint v$GOLANGCI_VERSION..."
    curl -sSfL "https://github.com/golangci/golangci-lint/releases/download/v${GOLANGCI_VERSION}/golangci-lint-${GOLANGCI_VERSION}-linux-amd64.tar.gz" | \
        tar -xzf - -C /tmp
    mv "/tmp/golangci-lint-${GOLANGCI_VERSION}-linux-amd64/golangci-lint" "$BINDIR/"
    echo "  Downloaded: $BINDIR/golangci-lint"
fi

# Download mage
MAGE_VERSION="1.15.0"
if [[ ! -f "$BINDIR/mage" ]]; then
    echo "Downloading mage v$MAGE_VERSION..."
    curl -sSfL "https://github.com/magefile/mage/releases/download/v${MAGE_VERSION}/mage_${MAGE_VERSION}_Linux-64bit.tar.gz" | \
        tar -xzf - -C /tmp
    mv /tmp/mage "$BINDIR/"
    echo "  Downloaded: $BINDIR/mage"
fi

# Download ripgrep
RG_VERSION="14.1.1"
if [[ ! -f "$BINDIR/rg" ]]; then
    echo "Downloading ripgrep v$RG_VERSION..."
    curl -sSfL "https://github.com/BurntSushi/ripgrep/releases/download/${RG_VERSION}/ripgrep-${RG_VERSION}-x86_64-unknown-linux-musl.tar.gz" | \
        tar -xzf - -C /tmp
    mv "/tmp/ripgrep-${RG_VERSION}-x86_64-unknown-linux-musl/rg" "$BINDIR/"
    echo "  Downloaded: $BINDIR/rg"
fi

echo ""
echo "=== Linux Binaries Ready ==="
ls -la "$BINDIR/"
