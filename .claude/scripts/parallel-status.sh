#!/bin/bash
# parallel-status.sh - Check status of parallel agent work
# Usage: ./parallel-status.sh

echo "=== Open PRs ==="
gh pr list --state open

echo ""
echo "=== Worktrees ==="
git worktree list

echo ""
echo "=== Agent Branches ==="
git branch | grep "agent-" || echo "None"

echo ""
echo "=== Recent Commits (agent branches) ==="
for branch in $(git branch --list "agent-*" | tr -d ' '); do
  echo "$branch: $(git log -1 --oneline $branch 2>/dev/null || echo 'no commits')"
done
