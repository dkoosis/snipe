#!/usr/bin/env bash
# risk-replay.sh — calibrate `snipe risk` verdict tiers against a repo's merge history.
#
# For each of the last N merge commits M in TARGET_REPO, this replays the PR:
#   checkout M (head) in an ISOLATED worktree → snipe index → snipe risk M^1 M
# and tabulates the verdict-tier distribution. Reindexing at each head is REQUIRED:
# a stale index can't map an old diff's file:line to symbols, so every historical
# PR falsely reads 0-symbol "low" (see sn-tgra). ~15s/reindex on trixi-sized repos.
#
# Usage: scripts/risk-replay.sh <target-repo> [N] [snipe-bin]
#   target-repo : path to the Go repo to replay (must have a .snipe index buildable)
#   N           : number of recent merges to replay (default 20)
#   snipe-bin   : path to snipe binary (default: build from this repo)
set -euo pipefail

# jq parses every `snipe risk` verdict below; without it each merge silently
# falls back to "low 0" and the whole calibration reads bogus. Fail loud instead.
command -v jq >/dev/null 2>&1 || { echo "error: jq is required but not installed" >&2; exit 1; }

TARGET="${1:?usage: risk-replay.sh <target-repo> [N] [snipe-bin]}"
N="${2:-20}"
SNIPE_REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SNIPE_BIN="${3:-}"

# Register cleanup BEFORE anything mktemp's, so a failed `go build` can't orphan files.
WT=""
CLEANUP_BIN=""
cleanup() {
  [[ -n "$WT" ]] && { git -C "$TARGET" worktree remove --force "$WT" 2>/dev/null || rm -rf "$WT"; }
  [[ -n "$CLEANUP_BIN" ]] && rm -f "$CLEANUP_BIN"
  return 0
}
trap cleanup EXIT

if [[ -z "$SNIPE_BIN" ]]; then
  SNIPE_BIN="$(mktemp -t snipe-risk-replay.XXXXXX)"
  CLEANUP_BIN="$SNIPE_BIN"  # we built it → we delete it (skip cleanup for a caller-supplied bin)
  echo "building snipe → $SNIPE_BIN" >&2
  (cd "$SNIPE_REPO" && go build -o "$SNIPE_BIN" .)
fi

TARGET="$(cd "$TARGET" && pwd)"
WT="$(mktemp -d -t risk-replay-wt.XXXXXX)"

# Detached worktree so the target's live checkout (possibly dirty, on a team branch) is untouched.
merges="$(git -C "$TARGET" log --merges -n "$N" --format='%H')"
git -C "$TARGET" worktree add --detach "$WT" HEAD >/dev/null 2>&1

# Prime the worktree's OWN index once (cold, ~2-3min). Every later checkout then
# reindexes INCREMENTALLY against this (~15s), since the index's repo_root now matches
# the worktree. Seeding from $TARGET/.snipe does NOT work: its baked-in repo_root points
# at the target, so snipe treats it as foreign and rebuilds cold each time.
echo "priming worktree index (cold, ~2-3min)…" >&2
( cd "$WT" && "$SNIPE_BIN" index --embed-mode=off --enrich=false >/dev/null 2>&1 ) || true

printf '%-10s  %-7s  %-6s  %-6s  %-6s  %s\n' MERGE VERDICT SCORE FILES SYMS REASONS
declare -A tier_count
i=0
# One flaky merge (no first parent, empty diff, jq miss) must not abort the sweep —
# every fallible step degrades to a skipped/low row instead of tripping `set -e`.
while read -r M; do
  [[ -z "$M" ]] && continue
  short="$(git -C "$TARGET" rev-parse --short "$M" 2>/dev/null || echo "${M:0:8}")"
  git -C "$WT" checkout -f "$M" >/dev/null 2>&1 || { echo "$short  SKIP (checkout failed)"; continue; }
  ( cd "$WT" && "$SNIPE_BIN" index --embed-mode=off --enrich=false >/dev/null 2>&1 ) || true
  json="$( (cd "$WT" && "$SNIPE_BIN" risk "${M}^1" "$M" --format json 2>/dev/null) || true )"
  line="$(echo "$json" | jq -r '.results[0] | "\(.verdict) \(.score) \(.changed.files) \(.changed.symbols) \((.reasons // []) | map(.signal) | join(","))"' 2>/dev/null || true)"
  read -r verdict score files syms reasons <<< "${line:-low 0 0 0 —}"
  printf '%-10s  %-7s  %-6s  %-6s  %-6s  %s\n' "$short" "${verdict:-low}" "${score:-0}" "${files:-0}" "${syms:-0}" "${reasons:-—}"
  tier_count[${verdict:-low}]=$(( ${tier_count[${verdict:-low}]:-0} + 1 ))
  i=$((i+1))
done <<< "$merges"

echo
echo "distribution over $i merges (tool's current cutoffs — high>=7 medium>=2 as of sn-tgra):"
for t in high medium low; do
  printf '  %-7s %s\n' "$t" "${tier_count[$t]:-0}"
done
