#!/bin/bash
# Environment diagnostic for Claude sandbox
# Wrapper that runs shared diagnostic from .codex/
# Output is structured for issue tracking

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Ensure Claude environment is activated
if [[ -z "${SNIPE_ENV_ACTIVATED:-}" ]]; then
    source "$REPO_ROOT/.claude/activate.sh" 2>/dev/null || true
fi

# Run shared diagnostic
exec "$REPO_ROOT/.codex/diagnose.sh" "$@"
