#!/bin/bash
# parallel-setup.sh - Create isolated git worktrees for parallel agents
# Usage: ./parallel-setup.sh [num_agents] [base_dir]

set -e

NUM=${1:-4}
BASE=${2:-"/tmp/snipe-agents"}
REPO=$(git rev-parse --show-toplevel)

echo "Creating $NUM worktrees at $BASE..."

git checkout main
git pull --ff-only

for i in $(seq 1 $NUM); do
  BRANCH="agent-$i"
  DIR="$BASE/$BRANCH"

  git branch -f "$BRANCH" main 2>/dev/null || true
  git worktree remove "$DIR" --force 2>/dev/null || true
  git worktree add "$DIR" "$BRANCH"

  echo "✓ $DIR → $BRANCH"
done

echo ""
echo "Launch:"
for i in $(seq 1 $NUM); do
  echo "  claude --cwd $BASE/agent-$i"
done
