package risk

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Signal code for the bead-DAG centrality signal (stable in the JSON contract).
const sigBeadCentral = "bead-central"

// Bead-centrality cutoffs (ported from ccp-ydmq bead-signal.sh; still uncalibrated —
// sn-tgra tunes them). A matched bead's dependent_count — how many beads it blocks —
// buckets the weight. We take the MAX over matched beads, never the sum: touching one
// foundational bead is the risk story, and summing would reward bead-spam in a commit.
const (
	beadCentralStrong = 3 // max dependent_count >= 3 → strong
	beadCentralWeak   = 1 // max dependent_count 1-2 → weak
)

// beadIDShape matches a bead-id-shaped token: a lowercase-led prefix, a final
// segment, and an optional dotted child index (sn-aa2, cc-plugins-are.2).
var beadIDShape = regexp.MustCompile(`^[a-z][a-z0-9-]*-[a-z0-9]+(\.[0-9]+)?$`)

// tokenSplit breaks candidate text on any run of non-[alnum.-] characters.
var tokenSplit = regexp.MustCompile(`[^a-zA-Z0-9.-]+`)

// bead is the subset of a .beads/issues.jsonl line this signal reads.
type bead struct {
	ID             string `json:"id"`
	DependentCount int    `json:"dependent_count"`
}

// beadsSignal folds bead-DAG centrality into the verdict when the repo carries a
// git-tracked .beads/issues.jsonl export. It scans the diff's own provenance — the
// head branch name and the base..head commit messages — for bead-id tokens, matches
// them against the export (exact, then alias-suffix), and buckets the max
// dependent_count of the matched beads. Every failure path returns nil silently: a
// repo without beads, or a diff naming none, contributes no signal.
//
// Ported from cc-plugins ccp-ydmq bead-signal.sh. The GitHub-event-payload surface
// (PR title/body) is intentionally dropped — snipe is a repo-agnostic CLI, and the
// judge that consumes the verdict owns any CI-metadata folding.
func beadsSignal(repoRoot, base, head string) []Signal {
	beads, ok := loadBeads(beadsFile(repoRoot))
	if !ok || len(beads) == 0 {
		return nil
	}
	text := changeProvenance(repoRoot, base, head)
	if text == "" {
		return nil
	}
	matched := matchBeads(text, beads)
	if len(matched) == 0 {
		return nil
	}

	maxDC, ids := 0, make([]string, 0, len(matched))
	for _, b := range matched {
		ids = append(ids, b.ID)
		if b.DependentCount > maxDC {
			maxDC = b.DependentCount
		}
	}
	weight := 0
	switch {
	case maxDC >= beadCentralStrong:
		weight = weightStrong
	case maxDC >= beadCentralWeak:
		weight = weightWeak
	default:
		return nil
	}

	sort.Strings(ids)
	return []Signal{{
		Signal: sigBeadCentral,
		Detail: fmt.Sprintf("touches bead(s) %s — one blocks %d others",
			strings.Join(ids, ","), maxDC),
		Weight: weight,
	}}
}

// beadsFile resolves the issues.jsonl path — the BEADS_FILE override, else the
// repo's default export location.
func beadsFile(repoRoot string) string {
	if p := os.Getenv("BEADS_FILE"); p != "" {
		return p
	}
	return filepath.Join(repoRoot, ".beads", "issues.jsonl")
}

// loadBeads reads the JSONL export into an id→bead map. A missing file returns
// (nil, false) — the caller degrades silently. Unparseable lines are skipped, not
// fatal. ok is true whenever the file opened, even if it held no valid beads.
func loadBeads(path string) (map[string]bead, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer func() { _ = f.Close() }()

	out := make(map[string]bead)
	sc := bufio.NewScanner(f)
	// Bead lines carry full descriptions; the 64 KB default token cap is too small.
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var b bead
		if err := json.Unmarshal(sc.Bytes(), &b); err != nil || b.ID == "" {
			continue
		}
		out[b.ID] = b
	}
	if err := sc.Err(); err != nil {
		// A truncated read still yields whatever parsed — partial beads beat none.
		return out, true
	}
	return out, true
}

// changeProvenance gathers the text that names the beads a diff belongs to: the head
// branch name and the commit messages unique to base..head. Refs starting with "-"
// are rejected (flag-injection guard, mirroring gitDiff). Any git failure yields a
// smaller-but-valid text — the signal degrades, never errors.
func changeProvenance(repoRoot, base, head string) string {
	if strings.HasPrefix(base, "-") || strings.HasPrefix(head, "-") {
		return ""
	}
	var sb strings.Builder
	// Head branch name — a bead id often rides the branch (feat/sn-aa2).
	if out, err := exec.Command("git", "-C", repoRoot, "rev-parse",
		"--abbrev-ref", "--end-of-options", head).Output(); err == nil {
		if br := strings.TrimSpace(string(out)); br != "" && br != "HEAD" {
			sb.WriteString(br)
			sb.WriteByte('\n')
		}
	}
	// Commit subjects + bodies for the commits head adds over base.
	if out, err := exec.Command("git", "-C", repoRoot, "log", "--format=%B",
		"--end-of-options", base+".."+head).Output(); err == nil {
		sb.Write(out)
	}
	return sb.String()
}

// matchBeads finds the beads named in text via two tiers: exact id match, then
// alias-suffix match (a short routing alias like sn-aa2 shares its final segment
// "aa2" with the canonical id snipe-aa2). The suffix tier requires the token to be
// bead-id-shaped and its final segment to bear a letter — rejecting utf-8, sha-256.
func matchBeads(text string, beads map[string]bead) []bead {
	bySuffix := make(map[string][]string, len(beads))
	for id := range beads {
		seg := finalSeg(id)
		bySuffix[seg] = append(bySuffix[seg], id)
	}

	seen := make(map[string]bool)
	var out []bead
	add := func(id string) {
		if !seen[id] {
			seen[id] = true
			out = append(out, beads[id])
		}
	}

	for _, tok := range tokenize(text) {
		if _, ok := beads[tok]; ok { // tier 1: exact
			add(tok)
			continue
		}
		if !beadIDShape.MatchString(tok) { // tier 2: alias suffix
			continue
		}
		seg := finalSeg(tok)
		if !hasLetter(baseSeg(seg)) {
			continue
		}
		for _, id := range bySuffix[seg] {
			add(id)
		}
	}
	return out
}

// tokenize splits text into unique candidate tokens, trimming trailing punctuation.
func tokenize(text string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, raw := range tokenSplit.Split(text, -1) {
		tok := strings.TrimRight(raw, ".,")
		if tok == "" || seen[tok] {
			continue
		}
		seen[tok] = true
		out = append(out, tok)
	}
	return out
}

// finalSeg returns the segment after the last '-' (the id's alias-bearing tail).
func finalSeg(s string) string {
	if i := strings.LastIndex(s, "-"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// baseSeg strips a dotted child index, leaving the letter-bearing base ("are.2"→"are").
func baseSeg(seg string) string {
	base, _, _ := strings.Cut(seg, ".")
	return base
}

// hasLetter reports whether s contains an ASCII lowercase letter.
func hasLetter(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 'a' && s[i] <= 'z' {
			return true
		}
	}
	return false
}
