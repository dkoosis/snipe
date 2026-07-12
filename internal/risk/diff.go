// Package risk turns a git diff into a code-graph risk verdict. It maps changed
// lines to changed symbols, fuses the signals snipe already owns (roles, package
// centrality, blast radius, churn), and emits one repo-agnostic verdict + reasons.
//
// Consumer: cc-plugins review-judge maps the verdict → a CI review tier. snipe owns
// the risk answer; the judge owns the policy. Every code path degrades silently to a
// low verdict rather than erroring, so the judge can call `snipe risk` on any repo.
package risk

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// FileChange is one changed file with its head-side (added/modified) line ranges.
// Ranges are inclusive [start, end] in head coordinates; pure deletions yield none.
type FileChange struct {
	Path       string
	LineRanges [][2]int
}

// gitDiff runs `git diff --unified=0 <base> <head> -- '*.go'` at repoRoot and
// returns the raw diff stream. ok is false — with no error — when git is absent,
// repoRoot is not a work tree, or a ref won't resolve, so callers degrade to a low
// verdict instead of failing (mirrors gitchurn.Walk's git-when-available contract).
func gitDiff(repoRoot, base, head string) (stream string, ok bool) {
	if _, err := exec.LookPath("git"); err != nil {
		return "", false
	}
	// A ref beginning with "-" would be parsed as a git option (e.g.
	// `--output=/path`), letting a caller-supplied ref inject flags. Refs never
	// legitimately start with "-", so reject rather than assess.
	if strings.HasPrefix(base, "-") || strings.HasPrefix(head, "-") {
		return "", false
	}
	if err := exec.Command("git", "-C", repoRoot, "rev-parse", "--is-inside-work-tree").Run(); err != nil {
		return "", false
	}
	// --end-of-options fixes base/head as operands even if a future ref form
	// looks option-like; the "--" then separates the *.go pathspec.
	out, err := exec.Command("git", "-C", repoRoot, "diff",
		"--unified=0", "--no-color", "--end-of-options", base, head, "--", "*.go").Output()
	if err != nil {
		return "", false
	}
	return string(out), true
}

// parseDiff turns a `git diff --unified=0` stream into per-file head-side line
// ranges. It tracks the current file from the `+++ b/<path>` header (so renames
// resolve to the head path) and reads each `@@ -a,b +c,d @@` hunk's + side. A
// deleted file (`+++ /dev/null`) or a zero-count head hunk contributes nothing.
func parseDiff(stream string) []FileChange {
	var out []FileChange
	var cur *FileChange

	flush := func() {
		if cur != nil && len(cur.LineRanges) > 0 {
			out = append(out, *cur)
		}
		cur = nil
	}

	for line := range strings.SplitSeq(stream, "\n") {
		switch {
		case strings.HasPrefix(line, "+++ "):
			flush()
			path := strings.TrimPrefix(line, "+++ ")
			if path == "/dev/null" {
				cur = nil
				continue
			}
			cur = &FileChange{Path: strings.TrimPrefix(path, "b/")}
		case strings.HasPrefix(line, "@@ ") && cur != nil:
			if start, count, ok := parseHunkHead(line); ok && count > 0 {
				cur.LineRanges = append(cur.LineRanges, [2]int{start, start + count - 1})
			}
		}
	}
	flush()
	return out
}

// parseHunkHead reads the head-side start line and count from `@@ -a,b +c,d @@`.
// `+c` with no comma means count 1; `+c,0` (pure deletion) yields count 0.
func parseHunkHead(line string) (start, count int, ok bool) {
	// Isolate the "+c,d" token between the "+" and the closing " @@".
	_, rest, found := strings.Cut(line, "+")
	if !found {
		return 0, 0, false
	}
	if before, _, ok := strings.Cut(rest, " "); ok {
		rest = before
	}
	startStr, countStr, hasComma := strings.Cut(rest, ",")
	start, err := strconv.Atoi(startStr)
	if err != nil {
		return 0, 0, false
	}
	count = 1
	if hasComma {
		if count, err = strconv.Atoi(countStr); err != nil {
			return 0, 0, false
		}
	}
	return start, count, true
}

// workingTreeDiff runs `git diff HEAD` (staged + unstaged combined, since both
// are compared straight against HEAD rather than against each other) at
// repoRoot, plus a separate untracked-file pass, and returns the same
// []FileChange shape gitDiff/parseDiff produce for a ref-to-ref diff.
// Untracked .go files are synthesized as whole-file changes spanning line 1
// to their line count — git diff never reports them since they're outside
// the index. ok is false — with no error — when git is absent or repoRoot
// isn't a work tree, mirroring gitDiff's degrade-to-low-verdict contract.
//
// Rename detection (-M) is explicit here so a `git mv` + edit resolves to one
// FileChange at the new path rather than a dropped delete plus a whole-file
// add; a pure rename with no content change yields no hunks and so
// contributes nothing, consistent with parseDiff dropping zero-range files.
func workingTreeDiff(repoRoot string) (changes []FileChange, ok bool) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, false
	}
	if err := exec.Command("git", "-C", repoRoot, "rev-parse", "--is-inside-work-tree").Run(); err != nil {
		return nil, false
	}
	out, err := exec.Command("git", "-C", repoRoot, "diff",
		"--unified=0", "--no-color", "-M", "HEAD", "--", "*.go").Output()
	if err != nil {
		// Most commonly an unborn HEAD (no commits yet); degrade rather than
		// fail, same as gitDiff's unresolved-ref case.
		return nil, false
	}
	changes = parseDiff(string(out))
	changes = append(changes, untrackedGoChanges(repoRoot)...)
	return changes, true
}

// untrackedGoChanges lists untracked .go files at repoRoot (relative to it)
// and synthesizes a whole-file FileChange for each non-empty one. Any git or
// filesystem error drops the offending file rather than failing the caller —
// this is a best-effort addition to workingTreeDiff, not a source of truth.
func untrackedGoChanges(repoRoot string) []FileChange {
	out, err := exec.Command("git", "-C", repoRoot, "ls-files",
		"--others", "--exclude-standard", "--", "*.go").Output()
	if err != nil {
		return nil
	}
	var changes []FileChange
	for path := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		if path == "" {
			continue
		}
		content, err := os.ReadFile(filepath.Join(repoRoot, path))
		if err != nil {
			continue
		}
		n := countLines(content)
		if n == 0 {
			continue
		}
		changes = append(changes, FileChange{Path: path, LineRanges: [][2]int{{1, n}}})
	}
	return changes
}

// countLines counts newline-delimited lines in content, counting a final
// unterminated line if present. An empty slice has zero lines.
func countLines(content []byte) int {
	if len(content) == 0 {
		return 0
	}
	n := bytes.Count(content, []byte("\n"))
	if content[len(content)-1] != '\n' {
		n++
	}
	return n
}

// changedGoFiles returns the paths of changed .go files, preserving order.
func changedGoFiles(changes []FileChange) []string {
	var out []string
	for _, c := range changes {
		if strings.HasSuffix(c.Path, ".go") {
			out = append(out, c.Path)
		}
	}
	return out
}
