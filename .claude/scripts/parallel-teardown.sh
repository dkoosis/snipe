#!/bin/bash
# parallel-teardown.sh - Remove worktrees, optionally delete branches
# Usage: ./parallel-teardown.sh [base_dir] [--delete-branches]

BASE=${1:-"/tmp/snipe-agents"}
DELETE_BRANCHES=$2

for dir in "$BASE"/agent-*; do
  [ -d "$dir" ] || continue
  branch=$(basename "$dir")

  git worktree remove "$dir" --force 2>/dev/null || true
  echo "✓ Removed $dir"

  [ "$DELETE_BRANCHES" = "--delete-branches" ] && git branch -D "$branch" 2>/dev/null
done

git worktree prune
echo "Done. Remaining worktrees:"
git worktree list
