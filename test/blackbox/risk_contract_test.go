//go:build blackbox

package blackbox

import (
	"os/exec"
	"testing"
)

// sn-n8re — golden contract test for `snipe risk --format json`.
//
// cc-plugins' review-judge (ccp-1cfm) is a thin consumer that hard-couples to
// this shape as a semver-guarded contract: it reads .results[0].{verdict,score,
// reasons,degraded} and branches on .degraded. A rename or a shape drift here
// silently miscalibrates their CI judge, so this test fails on any breaking
// change. The committed field set + the always-one-result guarantee ARE the
// contract; keep them stable or bump snipe's major version.
//
// The load-bearing distinction the judge needs: a clean analyzed diff
// (degraded=false, reasons=[]) means "trust it, tier:none", whereas an
// unanalyzable diff (degraded=true) means "drop to portable fallback, don't
// trust empty as safe". Both can surface reasons==[], so `degraded` — not
// len(reasons) — is the only sound discriminator. This test pins that.

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
	stdout, stderr, exitCode := run(t, repoDir, append([]string{"risk"}, args...)...)
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

	v := requireMap(t, results[0], "results[0]")
	// Stable field names the judge hard-codes. `note` is omitempty (absent on a
	// clean analyzed diff), so it isn't required here.
	if _, ok := v["verdict"].(string); !ok {
		t.Fatalf("risk %v: results[0].verdict missing/not a string: %v", args, v["verdict"])
	}
	if _, ok := v["score"].(float64); !ok {
		t.Fatalf("risk %v: results[0].score missing/not a number: %v", args, v["score"])
	}
	if _, ok := v["reasons"]; !ok {
		t.Fatalf("risk %v: results[0].reasons missing", args)
	}
	if _, ok := v["changed"].(map[string]any); !ok {
		t.Fatalf("risk %v: results[0].changed missing/not an object: %v", args, v["changed"])
	}
	if _, ok := v["degraded"].(bool); !ok {
		t.Fatalf("risk %v: results[0].degraded missing/not a bool: %v", args, v["degraded"])
	}
	return v
}

// reasonsLen returns the length of results[0].reasons, tolerating JSON null.
func reasonsLen(t *testing.T, v map[string]any) int {
	t.Helper()
	if v["reasons"] == nil {
		return 0
	}
	return len(requireSlice(t, v["reasons"], "reasons"))
}

func TestRiskJSONContract(t *testing.T) {
	repoDir, paths := writeFixture(t)
	repoDir = canonicalRepoDir(t, repoDir)
	initGitRepo(t, repoDir)

	// --- unanalyzable case 1: no index -------------------------------------
	// runRisk hits OpenStore before any diff work; a missing index degrades
	// rather than erroring. No indexRepo call yet.
	t.Run("missing_index_degrades", func(t *testing.T) {
		v := riskResult(t, repoDir, "HEAD")
		if v["degraded"] != true {
			t.Fatalf("missing index must degrade, got degraded=%v", v["degraded"])
		}
		if v["verdict"] != "low" {
			t.Fatalf("degraded verdict must be forced low, got %v", v["verdict"])
		}
	})

	indexRepo(t, repoDir)

	// --- unanalyzable case 2: no changed Go files (empty diff) --------------
	// This is the trap the judge must not fall into: reasons==[] here, but the
	// diff was NOT analyzed, so it must read degraded=true, never tier:none.
	var emptyDiff map[string]any
	t.Run("empty_diff_degrades", func(t *testing.T) {
		emptyDiff = riskResult(t, repoDir, "HEAD", "HEAD")
		if emptyDiff["degraded"] != true {
			t.Fatalf("empty diff (no Go files) must degrade, got degraded=%v", emptyDiff["degraded"])
		}
		if n := reasonsLen(t, emptyDiff); n != 0 {
			t.Fatalf("empty diff must carry no reasons, got %d", n)
		}
		if emptyDiff["verdict"] != "low" {
			t.Fatalf("degraded verdict must be forced low, got %v", emptyDiff["verdict"])
		}
	})

	// --- unanalyzable case 3: unresolved ref -------------------------------
	t.Run("unresolved_ref_degrades", func(t *testing.T) {
		v := riskResult(t, repoDir, "no-such-ref-deadbeef")
		if v["degraded"] != true {
			t.Fatalf("unresolved ref must degrade, got degraded=%v", v["degraded"])
		}
	})

	// --- analyzed clean case: real Go diff, no risk signals ----------------
	// Edit only a _test.go body: a changed Go file (so NOT degraded), but it
	// contributes no production symbols. This is the judge's "analyzed, trust
	// it" case; it must read degraded=false and be separable from the
	// unanalyzable empty diff above.
	editFile(t, paths["test"], readFile(t, paths["test"])+"\n// contract-probe touch\n")
	gitCommitAll(t, repoDir, "edit test only")
	indexRepo(t, repoDir)

	t.Run("degraded_is_the_discriminator", func(t *testing.T) {
		analyzed := riskResult(t, repoDir, "HEAD~1", "HEAD")
		t.Logf("analyzed verdict: %v", analyzed)
		if analyzed["degraded"] != false {
			t.Fatalf("a real analyzed diff must NOT be degraded, got degraded=%v", analyzed["degraded"])
		}
		if emptyDiff == nil {
			t.Fatal("empty-diff case did not run before discriminator check")
		}

		// The contract's load-bearing guarantee. The unanalyzable empty diff
		// carries reasons==[] (the trap: a judge keying on len(reasons)==0 as
		// "clean" would mis-score it as tier:none), yet it is degraded=true. The
		// analyzed diff is degraded=false. So `degraded` — never len(reasons) —
		// is the sound discriminator between "trust the clean verdict" and "drop
		// to portable fallback". In this small fixture the analyzed diff also
		// trips the file-level churn signal (every file lands in churn top-20),
		// so we can't manufacture an analyzed reasons==[]; the trap we DO pin is
		// the dangerous direction — empty-but-unanalyzable.
		if n := reasonsLen(t, emptyDiff); n != 0 {
			t.Fatalf("expected the unanalyzable empty diff to expose the trap (reasons==[]), got %d reasons", n)
		}
		if emptyDiff["degraded"] != true || analyzed["degraded"] != false {
			t.Fatalf("degraded must separate unanalyzable (want true) from analyzed (want false): "+
				"empty=%v analyzed=%v", emptyDiff["degraded"], analyzed["degraded"])
		}
	})
}
