# bd workflow — session completion

*Folded from the root CLAUDE.md stub (sd-mzgy.3); the rest of that file's content lives in this directory already.*

This project uses **bd (beads)** for issue tracking. Run `bd prime` for full workflow context.

- Use `bd` for ALL task tracking — not TodoWrite, TaskCreate, or markdown TODO lists.
- Use `bd remember` for persistent knowledge — not MEMORY.md files.

**Ending a session is not done until `git push` succeeds:**
1. File issues for remaining work.
2. Run quality gates (`make audit`) if code changed.
3. Close finished bd issues, update in-progress ones.
4. `git pull --rebase && bd dolt push && git push` — confirm `git status` shows up to date with origin.
5. Clean up stashes, prune remote branches.
6. Hand off context for the next session.
