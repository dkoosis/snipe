# Parallel Agent Task Template

‡CONTEXT:
- Isolated worktree: {WORKTREE_PATH}
- Branch: {BRANCH_NAME}
- Repo: snipe (Go CLI for code navigation)

‡CONSTRAINTS:
- Do NOT checkout other branches
- Do NOT cd outside this worktree
- Do NOT modify files outside your assigned set

∇TASKS:
{TASK_LIST}

→EXECUTION:
1. Read assigned files first
2. Make changes
3. `go build ./... && go test ./...`
4. Test manually if applicable
5. `git add -A && git commit -m "{COMMIT_MSG}"`
6. `git push -u origin {BRANCH_NAME}`
7. `gh pr create --title "{PR_TITLE}" --body "{PR_BODY}"`
8. Return: PR_URL

‡ON_ERROR:
- Build fail → fix and retry
- Test fail → fix and retry
- Push fail → check branch, retry
- PR fail → check if exists, return URL

→COMPLETE_WHEN: PR URL returned
