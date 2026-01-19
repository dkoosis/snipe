#!/bin/bash
# Environment diagnostic for Codex/Claude sandboxes
# Run this to report tool availability and versions
# Output is structured for easy issue tracking
set -u

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

ISSUES=()

echo "=== Snipe Sandbox Diagnostic ==="
echo "Timestamp: $(date -u +"%Y-%m-%dT%H:%M:%SZ")"
echo "Platform: $(uname -s)-$(uname -m)"
echo "Hostname: ${HOSTNAME:-$(hostname 2>/dev/null || echo unknown)}"
echo "Working dir: $REPO_ROOT"
echo ""

# Helper: check tool and record result
check_tool() {
    local name="$1"
    local cmd="$2"
    local version_cmd="$3"
    local required="$4"

    printf "%-16s " "$name:"

    if command -v "$cmd" &>/dev/null; then
        local tool_path=$(command -v "$cmd")
        local version=$($version_cmd 2>&1 | head -1 || echo "unknown")
        echo "OK  $version"
        echo "                 path: $tool_path"
    else
        if [[ "$required" == "yes" ]]; then
            echo "MISSING (required)"
            ISSUES+=("MISSING_REQUIRED: $name")
        else
            echo "MISSING (optional)"
        fi
    fi
}

# Helper: check file exists
check_file() {
    local name="$1"
    local filepath="$2"
    local required="$3"

    printf "%-16s " "$name:"
    if [[ -f "$filepath" ]]; then
        local filesize=$(ls -lh "$filepath" 2>/dev/null | awk '{print $5}')
        echo "OK ($filesize)"
    elif [[ -x "$filepath" ]]; then
        local filesize=$(ls -lh "$filepath" 2>/dev/null | awk '{print $5}')
        echo "OK ($filesize)"
    else
        if [[ "$required" == "yes" ]]; then
            echo "MISSING (required)"
            ISSUES+=("MISSING_FILE: $name at $filepath")
        else
            echo "MISSING (optional)"
        fi
    fi
}

echo "--- Core Tools ---"
check_tool "go" "go" "go version" "yes"
check_tool "git" "git" "git --version" "yes"

echo ""
echo "--- QA Tools ---"
check_tool "golangci-lint" "golangci-lint" "golangci-lint --version" "yes"
check_tool "govulncheck" "govulncheck" "govulncheck -version" "yes"
check_tool "mage" "mage" "mage -version" "yes"

echo ""
echo "--- Search Tools ---"
check_tool "rg (ripgrep)" "rg" "rg --version" "yes"
check_tool "snipe" "snipe" "snipe version" "yes"

echo ""
echo "--- Optional Tools ---"
check_tool "jq" "jq" "jq --version" "no"
check_tool "curl" "curl" "curl --version" "no"

echo ""
echo "--- Environment Variables ---"
printf "%-16s " "GOCACHE:"
if [[ -n "${GOCACHE:-}" ]]; then
    echo "$GOCACHE"
    [[ -d "$GOCACHE" ]] || echo "                 WARNING: directory does not exist"
else
    echo "(not set - using default)"
fi

printf "%-16s " "GOMODCACHE:"
if [[ -n "${GOMODCACHE:-}" ]]; then
    echo "$GOMODCACHE"
    [[ -d "$GOMODCACHE" ]] || echo "                 WARNING: directory does not exist"
else
    echo "(not set - using default)"
fi

printf "%-16s " "PATH includes:"
if [[ "$PATH" == *"$REPO_ROOT/bin"* ]]; then
    echo "./bin (good)"
else
    echo "./bin NOT in PATH"
    ISSUES+=("PATH_MISSING: ./bin not in PATH - run: source .codex/activate.sh")
fi

echo ""
echo "--- Snipe Index ---"
INDEX_PATH="$REPO_ROOT/.snipe/index.db"
printf "%-16s " "index.db:"
if [[ -f "$INDEX_PATH" ]]; then
    idx_size=$(ls -lh "$INDEX_PATH" 2>/dev/null | awk '{print $5}')
    # Cross-platform mtime
    if stat -c %Y "$INDEX_PATH" &>/dev/null; then
        idx_mtime=$(stat -c %Y "$INDEX_PATH")
    else
        idx_mtime=$(stat -f %m "$INDEX_PATH" 2>/dev/null || echo 0)
    fi
    now=$(date +%s)
    idx_age_hours=$(( (now - idx_mtime) / 3600 ))
    echo "OK  size=$idx_size  age=${idx_age_hours}h"

    # Quick sanity check with snipe doctor
    printf "%-16s " "snipe doctor:"
    if snipe doctor &>/dev/null; then
        echo "OK"
    else
        echo "FAILED"
        ISSUES+=("INDEX_UNHEALTHY: snipe doctor failed")
    fi
else
    echo "MISSING"
    ISSUES+=("MISSING_REQUIRED: snipe index - run: snipe index")
fi

echo ""
echo "--- Prebuilt Linux Binaries ---"
BINDIR="$REPO_ROOT/.codex/bin/linux-amd64"
if [[ -d "$BINDIR" ]]; then
    echo "Location: $BINDIR"
    check_file "  snipe" "$BINDIR/snipe" "yes"
    check_file "  golangci-lint" "$BINDIR/golangci-lint" "yes"
    check_file "  govulncheck" "$BINDIR/govulncheck" "yes"
    check_file "  mage" "$BINDIR/mage" "yes"
    check_file "  rg" "$BINDIR/rg" "yes"
else
    echo "Directory not found: $BINDIR"
    ISSUES+=("MISSING_BINDIR: $BINDIR - run: bash .codex/build-linux-binaries.sh")
fi

echo ""
echo "--- Quick Validation ---"

# Skip go build if prebuilt snipe binary is available and working
SKIP_BUILD=false
if [[ -x "$BINDIR/snipe" ]] || [[ -x "$REPO_ROOT/bin/snipe" ]]; then
    printf "%-16s " "go build:"
    echo "SKIPPED (prebuilt snipe available)"
    SKIP_BUILD=true
fi

if [[ "$SKIP_BUILD" == "false" ]]; then
    printf "%-16s " "go build:"
    BUILD_OUTPUT=$(go build -o /tmp/snipe-diag-test . 2>&1)
    BUILD_EXIT=$?
    if [[ $BUILD_EXIT -eq 0 ]]; then
        echo "OK"
        rm -f /tmp/snipe-diag-test
    else
        echo "FAILED (exit $BUILD_EXIT)"
        echo "                 Error: ${BUILD_OUTPUT:0:200}"
        ISSUES+=("BUILD_FAILED: go build failed - see error above")
    fi
fi

printf "%-16s " "go vet:"
VET_OUTPUT=$(go vet ./... 2>&1)
if [[ $? -eq 0 ]]; then
    echo "OK"
else
    echo "WARNINGS (non-blocking)"
    # Show first line of vet output for context
    [[ -n "$VET_OUTPUT" ]] && echo "                 ${VET_OUTPUT%%$'\n'*}"
fi

echo ""
echo "=========================================="
if [[ ${#ISSUES[@]} -eq 0 ]]; then
    echo "STATUS: ALL CHECKS PASSED"
    echo ""
    echo "Environment is ready for QA/testing."
    echo "Run: mage qa"
    exit 0
else
    echo "STATUS: ${#ISSUES[@]} ISSUE(S) FOUND"
    echo ""

    # Check if this is just a PATH issue (binaries exist but not on PATH)
    BINARIES_EXIST=true
    for bin in snipe golangci-lint govulncheck mage rg; do
        [[ ! -x "$BINDIR/$bin" ]] && BINARIES_EXIST=false
    done

    if [[ "$BINARIES_EXIST" == "true" ]] && [[ "$PATH" != *"$REPO_ROOT/bin"* ]]; then
        echo "QUICK FIX: Binaries exist but PATH not set."
        echo "Run: source .codex/activate.sh"
        echo "Then re-run: bash .codex/diagnose.sh"
        echo ""
    fi

    echo "Issues to fix:"
    for issue in "${ISSUES[@]}"; do
        echo "  - $issue"
    done
    echo ""
    echo "=========================================="
    echo "REPORT THIS OUTPUT if you cannot resolve."
    echo "=========================================="
    exit 1
fi
