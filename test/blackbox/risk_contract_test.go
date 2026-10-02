//go:build blackbox

package blackbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Golden contract test for `snipe risk --format json`.
//
// snipe risk hands over raw measures of a diff — no score, weights or verdict
// (sn-qtjl.4); callers (sdlc's review routing) decide what they mean. The
// committed field set + the always-one-result guarantee ARE the contract; keep
// them stable or bump snipe's major version.
//
// The load-bearing distinction: a measured diff (degraded=false) can be
// trusted, even when every measure is zero; an unmeasurable one (degraded=true)
// cannot. Zeros look the same in both, so `degraded` is the only sound
// discriminator. This test pins that.

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
	v, _ := riskResultEnv(t, repoDir, os.Environ(), args...)
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
	// Stable field names callers hard-code. `note` is omitempty (absent on a
	// measured diff), so it isn't required here.
	for _, f := range []string{"callers", "caller_files", "importers", "commits"} {
		if _, ok := v[f].(float64); !ok {
			t.Fatalf("risk %v: results[0].%s missing/not a number: %v", args, f, v[f])
		}
	}
	for _, f := range []string{"changed", "roles", "risk_flags"} {
		if _, ok := v[f].(map[string]any); !ok {
			t.Fatalf("risk %v: results[0].%s missing/not an object: %v", args, f, v[f])
		}
	}
	if _, ok := v["degraded"].(bool); !ok {
		t.Fatalf("risk %v: results[0].degraded missing/not a bool: %v", args, v["degraded"])
	}
	for _, gone := range []string{"verdict", "score", "reasons"} {
		if _, ok := v[gone]; ok {
			t.Fatalf("risk %v: results[0].%s is back; snipe hands over measures, not a judgment", args, gone)
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
	// Every measure is zero here, but nothing was measured: it must read
	// degraded=true, never as a measured change with zero reach.
	var emptyDiff map[string]any
	t.Run("empty_diff_degrades", func(t *testing.T) {
		emptyDiff = riskResult(t, repoDir, "HEAD", "HEAD")
		if emptyDiff["degraded"] != true {
			t.Fatalf("empty diff (no Go files) must degrade, got degraded=%v", emptyDiff["degraded"])
		}
		if emptyDiff["callers"] != 0.0 {
			t.Fatalf("empty diff must carry zero callers, got %v", emptyDiff["callers"])
		}
	})

	// --- unmeasurable case 3: unresolved ref -------------------------------
	t.Run("unresolved_ref_degrades", func(t *testing.T) {
		v := riskResult(t, repoDir, "no-such-ref-deadbeef")
		if v["degraded"] != true {
			t.Fatalf("unresolved ref must degrade, got degraded=%v", v["degraded"])
		}
	})

	// --- measured case with zero reach: a _test.go-only edit ---------------
	// A changed Go file (so NOT degraded) that contributes no production
	// symbols, so zero callers — the same zero the empty diff carries.
	editFile(t, paths["test"], readFile(t, paths["test"])+"\n// contract-probe touch\n")
	gitCommitAll(t, repoDir, "edit test only")
	indexRepo(t, repoDir)

	t.Run("degraded_is_the_discriminator", func(t *testing.T) {
		measured := riskResult(t, repoDir, "HEAD~1", "HEAD")
		t.Logf("measured: %v", measured)
		if emptyDiff == nil {
			t.Fatal("empty-diff case did not run before discriminator check")
		}
		// Both carry zero callers; only `degraded` tells "measured, nothing
		// reached" from "could not measure".
		if measured["callers"] != 0.0 || emptyDiff["callers"] != 0.0 {
			t.Fatalf("want zero callers in both: measured=%v empty=%v", measured["callers"], emptyDiff["callers"])
		}
		if emptyDiff["degraded"] != true || measured["degraded"] != false {
			t.Fatalf("degraded must separate unmeasurable (want true) from measured (want false): "+
				"empty=%v measured=%v", emptyDiff["degraded"], measured["degraded"])
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
		v, state := riskResultEnv(t, repoDir, os.Environ(), "HEAD~1", "HEAD")
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
		env := append(os.Environ(), "SNIPE_NO_HEAL=1")
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
		v, state := riskResultEnv(t, repoDir, os.Environ(), "HEAD~1", "HEAD")
		if v["degraded"] != false || state != "fresh" {
			t.Fatalf("healed index: want degraded=false index_state=fresh, got degraded=%v index_state=%q",
				v["degraded"], state)
		}
	})
}
