# Parallel Orchestrator Protocol

## Setup Phase
```bash
.claude/scripts/parallel-setup.sh {NUM_AGENTS}
```

## Launch Phase
For each agent, in separate terminal:
```bash
claude --cwd /tmp/snipe-agents/agent-{N} --prompt "$(cat <<'EOF'
{AGENT_PROMPT from parallel-agent.md with substitutions}
EOF
)"
```

## Monitor Phase
```bash
.claude/scripts/parallel-status.sh
gh pr list --state open
```

## Merge Phase
For each PR (in dependency order):
```bash
# 1. Check reviews
gh pr view {PR_NUM} --json reviews,state,mergeable

# 2. If CHANGES_REQUESTED → notify, skip
# 3. If mergeable:
gh pr merge {PR_NUM} --squash --delete-branch

# 4. Sync before next
git pull --ff-only
```

## Teardown Phase
```bash
.claude/scripts/parallel-teardown.sh --delete-branches
```

## File Assignment Strategy
- Assign disjoint file sets to avoid merge conflicts
- Group by package/concern
- Example:
  - Agent 1: cmd/search.go, cmd/refs.go
  - Agent 2: internal/query/position.go, internal/query/lookup.go
  - Agent 3: cmd/impl.go (new), cmd/pkg.go (new)
  - Agent 4: internal/output/types.go, internal/output/json.go
