// Package gitchurn derives per-file change-frequency from git history.
//
// Change-frequency is the temporal axis of code risk. Nagappan & Ball
// ("Use of Relative Code Churn Measures to Predict System Defect Density",
// ICSE 2005) showed that how often a file changes predicts its defect
// density better than size or complexity alone. Tornhill's hotspot model
// (Your Code as a Crime Scene, 2015; CodeScene) pairs this churn signal
// with complexity to rank risk — see the `snipe hotspots` command.
//
// The unit of change-frequency here is the number of non-merge commits that
// touched a file (CodeScene calls these "revisions"). The package is a thin,
// dependency-free wrapper over `git log`; it degrades to a clean no-op
// (nil, nil) outside a git repository, when git is absent, or on an unborn
// branch with no commits.
package gitchurn

import (
	"bufio"
	"os/exec"
	"strings"
	"time"
)

// halfLifeDays sets the exponential decay for the recency-weighted Score:
// a commit this old contributes half the weight of one made at the
// reference (newest) commit. Tornhill weights recent changes higher because
// stale churn is paid-down risk; 180 days is a pragmatic default.
const halfLifeDays = 180.0

// commitSentinel (SOH) prefixes each commit-header line in the git log
// stream so it can never collide with a file path. It is followed by the
// author date and author email; subsequent non-empty lines are file paths.
// Email (%aE) keys author identity rather than display name (%aN): the same
// person often commits under several names (user.name drift), which would
// inflate the distinct-author count. %aE also honors .mailmap, so the reverse
// — one person, several emails — coalesces on repos that maintain one.
const commitSentinel = "\x01"

// trailerSeparator (STX) joins a commit's repeated Bead-Type values onto the
// single header line. ADR 0002 rule 4 forbids comma-joining the trailers in
// the commit message itself; this only affects how git renders them here, and
// a control character can never appear inside a trailer value.
const trailerSeparator = "\x02"

// FileChurn is the git change-frequency record for one file, keyed by a
// repo-relative path so it joins directly against the symbols table.
type FileChurn struct {
	Path        string  // repo-relative path (matches symbols.file_path)
	Commits     int     // non-merge commits touching the file (CodeScene revisions)
	Authors     int     // distinct commit authors by email (bus-factor / knowledge-map signal)
	FirstSeen   string  // YYYY-MM-DD of the earliest touching commit
	LastChanged string  // YYYY-MM-DD of the latest touching commit
	Score       float64 // recency-weighted churn (see halfLifeDays)

	// Per-type commit counts, classified from each commit's Bead-Type
	// trailer (sdlc ADR 0002). Every touching commit lands in exactly one
	// bucket, so Bug+Feature+Chore+Other+Untyped == Commits.
	BugCommits     int // Bead-Type: bug — the defect-density signal
	FeatureCommits int // Bead-Type: feature
	ChoreCommits   int // Bead-Type: chore
	OtherCommits   int // Bead-Type: task|epic|spike — typed, but not one of the three above
	UntypedCommits int // no Bead-Type trailer, or a value outside the six known types
}

// beadType is a commit's classified Bead-Type bucket.
type beadType int

// Declared in ascending precedence order: classifyBeadTypes keeps the
// largest value it sees, so bug wins over every other type on a commit that
// serves several beads.
const (
	typeUntyped beadType = iota
	typeOther
	typeChore
	typeFeature
	typeBug
)

// classifyBeadTypes reduces a commit's Bead-Type values to one bucket.
// A commit serving several beads repeats the trailer (ADR 0002 rule 4), so
// several values can arrive at once; precedence is bug > feature > chore >
// other, making BugCommits read as "commits that fixed at least one bug" —
// the per-file defect count Tornhill approximates by grepping for "fix".
// An unrecognized value (including ADR 0002 rule 5's `Bead-ID: none` commits,
// which carry no type) is untyped: never guessed into a named bucket.
func classifyBeadTypes(values string) beadType {
	best := typeUntyped
	for _, v := range strings.Split(values, trailerSeparator) {
		var t beadType
		switch strings.TrimSpace(v) {
		case "bug":
			t = typeBug
		case "feature":
			t = typeFeature
		case "chore":
			t = typeChore
		case "task", "epic", "spike":
			t = typeOther
		default:
			continue
		}
		if t > best {
			best = t
		}
	}
	return best
}

// Walk returns the change-frequency of every tracked *.go file in repoRoot,
// sorted by commit count descending (ties broken by path). It returns
// (nil, nil) — never an error — when git is unavailable, repoRoot is not a
// work tree, or history is empty: a missing temporal axis is a clean no-op,
// not a failure, per the git-when-available constraint.
func Walk(repoRoot string) ([]FileChurn, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, nil //nolint:nilerr // git absent → no churn, not an error
	}
	// Confirm a work tree before logging so "not a repo" stays a no-op
	// rather than surfacing git's error text.
	if err := exec.Command("git", "-C", repoRoot, "rev-parse", "--is-inside-work-tree").Run(); err != nil {
		return nil, nil //nolint:nilerr // not a git work tree → no churn
	}

	// --no-renames keeps paths literal (no "{old => new}" arrows to parse);
	// the "-- *.go" pathspec restricts both the commits and the listed files
	// to Go sources, which is all snipe indexes.
	cmd := exec.Command("git", "-C", repoRoot, "log",
		"--no-merges", "--no-renames",
		"--pretty=tformat:"+commitSentinel+"%aI\t%aE\t"+
			"%(trailers:key=Bead-Type,valueonly,separator=%x02)",
		"--name-only", "--", "*.go")
	out, err := cmd.Output()
	if err != nil {
		// Most likely an unborn branch (no commits yet). Treat as empty.
		return nil, nil //nolint:nilerr // empty history → no churn
	}

	// Keep only files that still exist in the work tree and are first-party.
	// History includes long-deleted paths (misleading as hotspots, and they
	// never join the current symbols table), and vendor/ holds third-party
	// code snipe doesn't index — churn there isn't actionable.
	tracked := trackedGoFiles(repoRoot)
	rows := parseLog(string(out))
	live := rows[:0]
	for _, r := range rows {
		if strings.HasPrefix(r.Path, "vendor/") {
			continue
		}
		if tracked != nil {
			if _, ok := tracked[r.Path]; !ok {
				continue
			}
		}
		live = append(live, r)
	}
	return live, nil
}

// trackedGoFiles returns the set of currently-tracked *.go paths, or nil if
// the listing fails (in which case the caller keeps the unfiltered history).
func trackedGoFiles(repoRoot string) map[string]struct{} {
	out, err := exec.Command("git", "-C", repoRoot, "ls-files", "--", "*.go").Output()
	if err != nil {
		return nil
	}
	set := make(map[string]struct{})
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		if p := sc.Text(); p != "" {
			set[p] = struct{}{}
		}
	}
	return set
}

// parseLog turns the `git log --name-only` stream into per-file churn.
// The stream is newest-commit-first, so the first commit's date is the
// recency reference (weight 1.0) and decay grows with age.
func parseLog(stream string) []FileChurn {
	type acc struct {
		commits     int
		authors     map[string]struct{}
		first, last time.Time
		score       float64
		byType      [typeBug + 1]int
	}
	byPath := make(map[string]*acc)

	var (
		curDate  time.Time
		curEmail string
		curType  beadType
		haveCur  bool
		refDate  time.Time // newest commit date, set from the first header
	)

	sc := bufio.NewScanner(strings.NewReader(stream))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, commitSentinel) {
			rest := line[len(commitSentinel):]
			fields := strings.SplitN(rest, "\t", 3)
			iso := fields[0]
			var email, trailers string
			if len(fields) > 1 {
				email = fields[1]
			}
			if len(fields) > 2 {
				trailers = fields[2]
			}
			t, err := time.Parse(time.RFC3339, iso)
			if err != nil {
				haveCur = false
				continue
			}
			curDate, curEmail, haveCur = t, email, true
			curType = classifyBeadTypes(trailers)
			if refDate.IsZero() {
				refDate = t
			}
			continue
		}
		if line == "" || !haveCur {
			continue
		}
		path := line
		a := byPath[path]
		if a == nil {
			a = &acc{authors: make(map[string]struct{}), first: curDate, last: curDate}
			byPath[path] = a
		}
		a.commits++
		a.authors[curEmail] = struct{}{}
		a.byType[curType]++
		if curDate.Before(a.first) {
			a.first = curDate
		}
		if curDate.After(a.last) {
			a.last = curDate
		}
		ageDays := refDate.Sub(curDate).Hours() / 24.0
		a.score += pow2(-ageDays / halfLifeDays)
	}

	out := make([]FileChurn, 0, len(byPath))
	for p, a := range byPath {
		out = append(out, FileChurn{
			Path:        p,
			Commits:     a.commits,
			Authors:     len(a.authors),
			FirstSeen:   a.first.Format("2006-01-02"),
			LastChanged: a.last.Format("2006-01-02"),
			Score:       a.score,

			BugCommits:     a.byType[typeBug],
			FeatureCommits: a.byType[typeFeature],
			ChoreCommits:   a.byType[typeChore],
			OtherCommits:   a.byType[typeOther],
			UntypedCommits: a.byType[typeUntyped],
		})
	}
	sortByCommits(out)
	return out
}
