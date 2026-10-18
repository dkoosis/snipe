//go:build blackbox

package blackbox

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Golden contract test for `snipe risk --format json`.
//
// snipe risk hands the router measures in five factors (reach, difficulty,
// history, kind, safety net), each scored 0-1, and one overall score from
// visible, versioned weights — no band or verdict (sn-qtjl.4). The committed
// field set + the always-one-result guarantee ARE the contract; keep them
// stable or bump snipe's major version.
//
// The load-bearing distinction: a measured diff (degraded=false) carries a
// score, even a low one; an unmeasurable diff (degraded=true) carries none —
// score is null, never 0, so a router can't read "couldn't look" as "safe".

// gitCommitAll stages everything and commits, with ambient hooks disabled (the
// global commit-msg conventional-commit hook otherwise rejects fixture commits).
func gitCommitAll(t *testing.T, dir, msg string) {
	t.Helper()
	for _, args := range [][]string{
		{"add", "-A"},
		{"commit", "-m", msg},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// riskResult runs `snipe risk <args> --format json`, asserts the invariant shell
// (exit 0, single result, meta.total==1, stable field set) that holds on EVERY
// path — clean, degraded, or unanalyzable — and returns the sole verdict object.
func riskResult(t *testing.T, repoDir string, args ...string) map[string]any {
	t.Helper()
	v, _ := riskResultEnv(t, repoDir, snipeEnv(), args...)
	return v
}

// riskResultEnv is riskResult under a given environment; it also returns
// meta.index_state, which is never empty.
func riskResultEnv(t *testing.T, repoDir string, env []string, args ...string) (verdict map[string]any, indexState string) {
	t.Helper()
	stdout, stderr, exitCode := runWithEnv(t, repoDir, env, append([]string{"risk"}, args...)...)
	if exitCode != 0 {
		t.Fatalf("risk %v exit %d (risk must never fail — it degrades): stderr=%s stdout=%s",
			args, exitCode, string(stderr), string(stdout))
	}
	resp := assertEnvelope(t, stdout, "risk")

	// Always exactly one result — risk never emits results==[]. A consumer can
	// unconditionally read .results[0]; the judge relies on this.
	results := requireSlice(t, resp["results"], "results")
	if len(results) != 1 {
		t.Fatalf("risk %v: want exactly 1 result, got %d (contract: always one)", args, len(results))
	}
	meta := requireMap(t, resp["meta"], "meta")
	if total, ok := meta["total"].(float64); !ok || total != 1 {
		t.Fatalf("risk %v: meta.total = %v, want 1", args, meta["total"])
	}
	indexState, _ = meta["index_state"].(string)
	if indexState == "" {
		t.Fatalf("risk %v: meta.index_state is empty, want fresh, stale or missing", args)
	}

	v := requireMap(t, results[0], "results[0]")
	// Stable field names callers hard-code. `note` is omitempty; `score` and
	// `history` may be null.
	if _, ok := v["score"]; !ok {
		t.Fatalf("risk %v: results[0].score missing", args)
	}
	if _, ok := v["weights_version"].(float64); !ok {
		t.Fatalf("risk %v: results[0].weights_version missing/not a number: %v", args, v["weights_version"])
	}
	for _, f := range []string{"factors", "weights", "changed", "reach", "difficulty", "kind", "tests"} {
		if _, ok := v[f].(map[string]any); !ok {
			t.Fatalf("risk %v: results[0].%s missing/not an object: %v", args, f, v[f])
		}
	}
	if _, ok := v["history"]; !ok {
		t.Fatalf("risk %v: results[0].history missing", args)
	}
	if _, ok := v["focus"].([]any); !ok {
		t.Fatalf("risk %v: results[0].focus missing/not an array: %v", args, v["focus"])
	}
	if _, ok := v["degraded"].(bool); !ok {
		t.Fatalf("risk %v: results[0].degraded missing/not a bool: %v", args, v["degraded"])
	}
	if v["degraded"] == true && v["score"] != nil {
		t.Fatalf("risk %v: degraded result carries score %v; it must be null", args, v["score"])
	}
	for _, gone := range []string{"verdict", "reasons"} {
		if _, ok := v[gone]; ok {
			t.Fatalf("risk %v: results[0].%s is back; snipe hands over a score and its measures, not a band", args, gone)
		}
	}
	return v, indexState
}

func TestRiskJSONContract(t *testing.T) {
	repoDir, paths := writeFixture(t)
	repoDir = canonicalRepoDir(t, repoDir)
	initGitRepo(t, repoDir)

	// --- unmeasurable case 1: no index ------------------------------------
	// runRisk hits OpenStore before any diff work; a missing index degrades
	// rather than erroring. No indexRepo call yet.
	t.Run("missing_index_degrades", func(t *testing.T) {
		v := riskResult(t, repoDir, "HEAD")
		if v["degraded"] != true {
			t.Fatalf("missing index must degrade, got degraded=%v", v["degraded"])
		}
	})

	indexRepo(t, repoDir)

	// --- unmeasurable case 2: no changed Go files (empty diff) -------------
	// Nothing was measured: it must read degraded=true with a null score,
	// never as a measured change that scored low.
	var emptyDiff map[string]any
	t.Run("empty_diff_degrades", func(t *testing.T) {
		emptyDiff = riskResult(t, repoDir, "HEAD", "HEAD")
		if emptyDiff["degraded"] != true {
			t.Fatalf("empty diff (no Go files) must degrade, got degraded=%v", emptyDiff["degraded"])
		}
		if emptyDiff["score"] != nil {
			t.Fatalf("empty diff must carry no score, got %v", emptyDiff["score"])
		}
	})

	// --- unmeasurable case 3: unresolved ref -------------------------------
	t.Run("unresolved_ref_degrades", func(t *testing.T) {
		v := riskResult(t, repoDir, "no-such-ref-deadbeef")
		if v["degraded"] != true {
			t.Fatalf("unresolved ref must degrade, got degraded=%v", v["degraded"])
		}
	})

	// --- measured case: a _test.go-only edit --------------------------------
	// A changed Go file (so NOT degraded) that contributes no production
	// symbols: a measured, low score — unlike the empty diff's null.
	editFile(t, paths["test"], readFile(t, paths["test"])+"\n// contract-probe touch\n")
	gitCommitAll(t, repoDir, "edit test only")
	indexRepo(t, repoDir)

	t.Run("degraded_is_the_discriminator", func(t *testing.T) {
		measured := riskResult(t, repoDir, "HEAD~1", "HEAD")
		t.Logf("measured: %v", measured)
		if emptyDiff == nil {
			t.Fatal("empty-diff case did not run before discriminator check")
		}
		score, ok := measured["score"].(float64)
		if !ok || score < 0 || score > 1 {
			t.Fatalf("measured diff: want a score in [0,1], got %v", measured["score"])
		}
		if emptyDiff["score"] != nil {
			t.Fatalf("unmeasurable diff: want a null score, got %v", emptyDiff["score"])
		}
		if emptyDiff["degraded"] != true || measured["degraded"] != false {
			t.Fatalf("degraded must separate unmeasurable (want true) from measured (want false): "+
				"empty=%v measured=%v", emptyDiff["degraded"], measured["degraded"])
		}
	})

	// --- each answer is kept ------------------------------------------------
	// snipe keeps what it said, raw measures included, so its weights can be
	// judged against review outcomes later (sn-qtjl.5): one run, one record.
	t.Run("run_appends_one_record", func(t *testing.T) {
		var env []string
		for _, e := range snipeEnv() {
			if !strings.HasPrefix(e, "SNIPE_NO_TELEMETRY=") {
				env = append(env, e)
			}
		}
		recPath := filepath.Join(repoDir, ".snipe", "risk.jsonl")
		before := riskRecords(t, recPath)
		riskResultEnv(t, repoDir, env, "HEAD~1", "HEAD")
		after := riskRecords(t, recPath)
		if len(after) != len(before)+1 {
			t.Fatalf("one risk run: want %d records, got %d", len(before)+1, len(after))
		}
		rec := after[len(after)-1]
		if rec["base"] != "HEAD~1" || rec["head"] != "HEAD" {
			t.Fatalf("record refs: want HEAD~1..HEAD, got %v..%v", rec["base"], rec["head"])
		}
		if rec["base_sha"] != gitRevParse(t, repoDir, "HEAD~1") || rec["head_sha"] != gitRevParse(t, repoDir, "HEAD") {
			t.Fatalf("record SHAs: got base=%v head=%v", rec["base_sha"], rec["head_sha"])
		}
		if rec["index_state"] != "fresh" {
			t.Fatalf("record index_state: want fresh, got %v", rec["index_state"])
		}
		if _, ok := rec["score"].(float64); !ok {
			t.Fatalf("record of a measured diff: want a score, got %v", rec["score"])
		}
		if _, ok := rec["weights_version"].(float64); !ok {
			t.Fatalf("record weights_version missing: %v", rec["weights_version"])
		}
		for _, f := range []string{"factors", "weights", "changed", "reach", "difficulty", "kind", "tests"} {
			if _, ok := rec[f].(map[string]any); !ok {
				t.Fatalf("record %s missing/not an object: %v", f, rec[f])
			}
		}
	})

	// --- stale index: the diff is real, but the index cannot map it --------
	// A stale index finds too few changed symbols and reads as a small, safe
	// change (sn-qtjl.2: every sdlc PR scored low with 0 symbols). It must
	// degrade, so the judge drops to its fallback instead of tier:none.
	goMod := filepath.Join(repoDir, "go.mod")
	origGoMod := readFile(t, goMod)
	editFile(t, goMod, origGoMod+"\n// stale-probe\n")
	t.Run("changed_go_mod_degrades", func(t *testing.T) {
		v, state := riskResultEnv(t, repoDir, snipeEnv(), "HEAD~1", "HEAD")
		if v["degraded"] != true || state != "stale" {
			t.Fatalf("index older than go.mod: want degraded=true index_state=stale, got degraded=%v index_state=%q",
				v["degraded"], state)
		}
		if note, _ := v["note"].(string); !strings.Contains(note, "index is stale") {
			t.Fatalf("note must name the stale index, got %q", note)
		}
	})
	editFile(t, goMod, origGoMod)

	// Drift the inline heal does not repair (here: heal disabled; in the wild:
	// more than 20 changed files, or a heal that timed out).
	editFile(t, paths["test"], readFile(t, paths["test"])+"\n// drift-probe\n")
	// Change detection reads mtime in whole seconds; move it past the index's.
	later := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(paths["test"], later, later); err != nil {
		t.Fatal(err)
	}
	t.Run("unhealed_drift_degrades", func(t *testing.T) {
		env := append(snipeEnv(), "SNIPE_NO_HEAL=1")
		v, state := riskResultEnv(t, repoDir, env, "HEAD~1", "HEAD")
		if v["degraded"] != true || state != "stale" {
			t.Fatalf("drifted index: want degraded=true index_state=stale, got degraded=%v index_state=%q",
				v["degraded"], state)
		}
		if note, _ := v["note"].(string); !strings.Contains(note, "1 file changed") {
			t.Fatalf("note must say how far the index drifted, got %q", note)
		}
	})

	// The same drift, healed inline, scores normally against a fresh index.
	t.Run("healed_drift_is_fresh", func(t *testing.T) {
		v, state := riskResultEnv(t, repoDir, snipeEnv(), "HEAD~1", "HEAD")
		if v["degraded"] != false || state != "fresh" {
			t.Fatalf("healed index: want degraded=false index_state=fresh, got degraded=%v index_state=%q",
				v["degraded"], state)
		}
	})
}

// riskRecords reads .snipe/risk.jsonl, one object per line; none when absent.
func riskRecords(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- test fixture path
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("risk.jsonl line %q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

func gitRevParse(t *testing.T, dir, ref string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", ref)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse %s: %v", ref, err)
	}
	return strings.TrimSpace(string(out))
}
