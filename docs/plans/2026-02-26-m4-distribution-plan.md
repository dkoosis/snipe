# M4 Distribution Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make snipe installable via GitHub Releases with tagged binaries and a clear first-run experience.

**Architecture:** goreleaser builds macOS + Linux binaries on tag push via GitHub Actions. ldflags stamp version/commit into existing `cmd/version.go` vars. First-run error message guides new users.

**Tech Stack:** goreleaser v2, GitHub Actions, Go ldflags

---

### Task 1: Create `.goreleaser.yml`

**Files:**
- Create: `.goreleaser.yml`

**Step 1: Create goreleaser config**

```yaml
---
version: 2
project_name: snipe

before:
  hooks:
    - go mod download

builds:
  - id: snipe
    main: .
    goos:
      - darwin
      - linux
    goarch:
      - amd64
      - arm64
    ignore:
      - goos: linux
        goarch: arm64
    flags:
      - -trimpath
    ldflags:
      - "-s -w -X github.com/dkoosis/snipe/cmd.Version={{ .Version }} -X github.com/dkoosis/snipe/cmd.GitCommit={{ .ShortCommit }}"

archives:
  - name_template: "{{ .ProjectName }}-{{ .Version }}-{{ .Os }}_{{ .Arch }}"
    formats:
      - tar.gz

checksum:
  name_template: "checksums.txt"

release:
  github:
    owner: dkoosis
    name: snipe
  prerelease: auto
  name_template: "v{{ .Version }}"

changelog:
  sort: asc
  filters:
    exclude:
      - "^docs:"
      - "^chore:"
      - "^test:"
```

**Step 2: Validate config locally**

Run: `goreleaser check` (install with `go install github.com/goreleaser/goreleaser/v2@latest` if needed)
Expected: valid config, no errors

**Step 3: Dry-run build**

Run: `goreleaser build --snapshot --clean`
Expected: binaries in `dist/` for darwin_amd64, darwin_arm64, linux_amd64

**Step 4: Verify ldflags stamping**

Run: `./dist/snipe_darwin_arm64/snipe version`
Expected: `snipe version 0.1.0-SNAPSHOT-<hash> (commit: <hash>)`

**Step 5: Commit**

```bash
git add .goreleaser.yml
git commit -m "feat: add goreleaser config for macOS + Linux builds"
```

---

### Task 2: Create GitHub Actions release workflow

**Files:**
- Create: `.github/workflows/release.yml`

**Step 1: Create workflow**

```yaml
name: Release

on:
  push:
    tags:
      - "v*"

permissions:
  contents: write

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod

      - uses: goreleaser/goreleaser-action@v6
        with:
          version: "~> v2"
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

**Step 2: Validate YAML syntax**

Run: `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/release.yml'))"`
Expected: no errors

**Step 3: Commit**

```bash
git add .github/workflows/release.yml
git commit -m "ci: add GitHub Actions release workflow for tagged builds"
```

---

### Task 3: Update first-run error message

**Files:**
- Modify: `internal/output/types.go:247-256`
- Update: `internal/output/testdata/missing_index_output.json` (golden file)

**Step 1: Update `NewMissingIndexError()` in `internal/output/types.go`**

Change message from:
```
No index found. Run 'snipe index' first to build it (takes ~5 seconds for most Go projects).
```

To:
```
No index found. Run: snipe index (~5s for most projects). The .snipe/ directory will be created — add it to .gitignore.
```

Keep the `Next` action unchanged.

**Step 2: Update golden file**

Run: `go test ./internal/output/ -update` or manually update the golden JSON to match.
If no `-update` flag exists, run the test to see the diff and update `testdata/missing_index_output.json` by hand.

**Step 3: Run tests**

Run: `mage`
Expected: all pass including golden test and blackbox test

**Step 4: Commit**

```bash
git add internal/output/types.go internal/output/testdata/missing_index_output.json
git commit -m "fix: improve first-run error message with .gitignore guidance"
```

---

### Task 4: Add README release badge

**Files:**
- Modify: `README.md:1-3`

**Step 1: Add badge after title**

Change:
```markdown
# snipe

Go code navigation for LLMs. Static indexing, <50ms queries, JSON output.
```

To:
```markdown
# snipe

[![Release](https://img.shields.io/github/v/release/dkoosis/snipe)](https://github.com/dkoosis/snipe/releases)

Go code navigation for LLMs. Static indexing, <50ms queries, JSON output.
```

**Step 2: Commit**

```bash
git add README.md
git commit -m "docs: add GitHub release badge to README"
```

---

### Task 5: Add `dist/` to `.gitignore`

**Files:**
- Modify: `.gitignore`

**Step 1: Add goreleaser output directory**

Add to `.gitignore`:
```
# goreleaser
dist/
```

**Step 2: Commit**

```bash
git add .gitignore
git commit -m "chore: gitignore goreleaser dist/ directory"
```

---

### Task 6: Final verification

**Step 1: Run full QA**

Run: `mage qa`
Expected: all 7 tasks pass

**Step 2: Verify version command still works**

Run: `snipe version` and `snipe version --json`
Expected: version 0.1.0, commit unknown (ldflags only apply in goreleaser builds)

**Step 3: Verify goreleaser snapshot**

Run: `goreleaser build --snapshot --clean`
Expected: 3 binaries in `dist/`, version stamped

---

### Post-merge: Tag and release

After all tasks merged to main:

```bash
git tag v0.1.0
git push origin v0.1.0
```

GitHub Actions will build and publish to Releases automatically.
Verify at: `https://github.com/dkoosis/snipe/releases`
