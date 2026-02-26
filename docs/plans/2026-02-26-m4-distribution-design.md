# M4: Distribution Design

## Scope

Make snipe installable by someone who isn't you, in under 60 seconds.

## Deliverables

### 1. goreleaser + GitHub Actions

`.goreleaser.yml`: macOS (arm64, amd64) + Linux (amd64). ldflags stamp `Version` and `GitCommit` via `cmd/version.go`. `-trimpath` for reproducible builds. No signing, no notarization.

`.github/workflows/release.yml`: triggered on `v*` tags. Uses `goreleaser/goreleaser-action`. Single job: checkout, setup-go, goreleaser release.

`go install github.com/dkoosis/snipe@latest` already works today — unaffected.

### 2. First-run prompt

Tighten `NewMissingIndexError()` in `internal/output/types.go`:

> No index found. Run: snipe index (~5s for most projects). The .snipe/ directory will be created — add it to .gitignore.

No auto-indexing. No auto-gitignore modification. Clear prompt, user acts.

### 3. README badge

Add GitHub release badge at top of README.md after releases exist.

## Not in scope (future)

- Windows builds
- macOS notarization/signing
- Homebrew tap
- Auto-indexing on first query
- Auto-modifying .gitignore
- `go install` version stamping via `debug.ReadBuildInfo`

## Success criteria

- `git tag v0.1.0 && git push --tags` triggers build + release
- `snipe --version` prints real semver + commit on release binaries
- GitHub Releases page has downloadable macOS + Linux binaries
- Missing-index error clearly tells new user what to do
