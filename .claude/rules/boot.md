sha: 589f6c7
updated: 2026-02-05T14:38:15Z
qa: pass

ready: working tree has uncommitted stale batch recovery + purpose preservation changes
- cmd/embed.go: stale batch age/staleness fields in EmbedStatusResult
- cmd/index.go: smart stale batch recovery with Voyage API verification before clearing
- cmd/root.go: fix embed -> embed-status in knownSubcommands
- internal/store/write.go: preserveSymbolPurposes/restoreSymbolPurposes across reindex
- internal/index/changes.go: nilerr lint fixes for PR #82's DetectChanges
- all changes committed and pushed as 589f6c7

done:
- reviewed open PRs (none open), synced main with origin (PR #82 incremental indexing)
- fixed 5 nilerr lint violations from PR #82 @internal/index/changes.go @cmd/index.go:514
- added stale batch detection with API verification @cmd/index.go:379
- added symbol purpose preservation across reindex @internal/store/write.go:211
- fixed embed-status subcommand name @cmd/root.go:153
- dropped 6 stale stashes (dead branches, duplicate BASELINE noise)
- cleaned up remote branches (already pruned)
- committed and pushed 589f6c7
