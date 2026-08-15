package checks

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Surface 2 (--local): machine wiring CI can't see. A fresh clone passes
// Surface 1 with every gate dead — hooksPath unset, bd unhydrated. This
// surface checks the live half: run it from `make doctor` and at session
// start, on the developer's machine.
const (
	// RuleHooksPath — core.hooksPath resolves to .githooks (shape B live).
	RuleHooksPath = "hooks-path"
	// RuleBDHooks — every tracked hook is executable and delegates to bd's
	// machinery (the managed BEADS INTEGRATION block).
	RuleBDHooks = "bd-hooks"
	// RuleReviewGate — the pre-push chain is alive end to end: hooksPath set,
	// pre-push executable, delegation present. (Which gate implementation it
	// chains is cfm-1e1.6's consolidation, not checked here.)
	RuleReviewGate = "review-gate"
	// RuleDoltRemote — bd's LIVE sync.remote is configured, so bead history
	// has an off-machine path.
	RuleDoltRemote = "dolt-remote"
	// RuleBDConfigLive — bd's live config matches the tracked declaration
	// (.beads/config.yaml) that Surface 1 checks. Two homes exist by
	// necessity (the live store is per-machine); this rule is what keeps
	// them from drifting.
	RuleBDConfigLive = "bd-config-live"
	// RuleNoGitOps is exception-only vocabulary: a repo that declares
	// {"rule": "no-git-ops"} (loto — confirmed deliberate) excepts the whole
	// git-hook family in one word. No check emits this id.
	RuleNoGitOps = "no-git-ops"
)

// noGitOpsFamily is what a no-git-ops exception expands to.
var noGitOpsFamily = []string{RuleHooksShape, RuleHooksPath, RuleBDHooks, RuleReviewGate}

// trackedHookEvents are the bd hook events shape B tracks in .githooks.
var trackedHookEvents = []string{"post-checkout", "post-merge", "pre-commit", "pre-push", "prepare-commit-msg"}

// delegationMarker identifies the beads-managed delegation block inside a
// tracked hook. Checked semantically (marker presence), not byte-wise — the
// block's body is bd's to version.
const delegationMarker = "BEADS INTEGRATION"

const (
	gitTimeout = 2 * time.Second
	bdTimeout  = 2 * time.Second
)

// RunLocal executes all Surface-2 checks against the repo rooted at dir,
// with the repo's declared exceptions filtered out (a no-git-ops exception
// covers the whole hook family).
func RunLocal(ctx context.Context, dir string) []Finding {
	vals, findings := loadValues(dir)

	findings = append(findings, checkHooksPath(ctx, dir)...)
	findings = append(findings, checkBDHooks(dir)...)
	findings = append(findings, checkReviewGate(ctx, dir)...)
	findings = append(findings, checkBDLive(ctx, dir)...)

	findings = applyExceptions(findings, vals)
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Rule < findings[j].Rule
	})
	return findings
}

// hooksPathValue resolves core.hooksPath as git sees it (all config scopes).
func hooksPathValue(ctx context.Context, dir string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "config", "--get", "core.hooksPath")
	cmd.Dir = dir
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	err := cmd.Run()
	return strings.TrimSpace(stdout.String()), err
}

// checkHooksPath verifies the live half of shape B: git actually reads hooks
// from the tracked .githooks (hooks-path).
func checkHooksPath(ctx context.Context, dir string) []Finding {
	val, err := hooksPathValue(ctx, dir)
	switch {
	case err != nil || val == "":
		return []Finding{{
			File:   ".git/config",
			Rule:   RuleHooksPath,
			Msg:    "core.hooksPath unset — git reads .git/hooks (empty on every clone); every tracked gate is dead",
			Repair: "git config core.hooksPath .githooks",
		}}
	case val != hooksDir:
		return []Finding{{
			File:   ".git/config",
			Rule:   RuleHooksPath,
			Msg:    fmt.Sprintf("core.hooksPath is %q, want %q — hooks fire from an unreviewed location (shape A is retired)", val, hooksDir),
			Repair: "git config core.hooksPath .githooks",
		}}
	}
	return nil
}

// checkBDHooks verifies every tracked hook event is present, executable, and
// delegates to bd (bd-hooks).
func checkBDHooks(dir string) []Finding {
	var findings []Finding
	for _, event := range trackedHookEvents {
		rel := filepath.Join(hooksDir, event)
		path := filepath.Join(dir, rel)
		fi, err := os.Stat(path)
		if err != nil {
			findings = append(findings, Finding{
				File:   rel,
				Rule:   RuleBDHooks,
				Msg:    event + " hook missing — bd's " + event + " machinery never runs on this machine",
				Repair: "bd hooks install (then commit the .githooks sources)",
			})
			continue
		}
		if fi.Mode()&0o111 == 0 {
			findings = append(findings, Finding{
				File:   rel,
				Rule:   RuleBDHooks,
				Msg:    "hook is not executable — git skips it without a word",
				Repair: "chmod +x " + rel,
			})
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil || !bytes.Contains(data, []byte(delegationMarker)) {
			findings = append(findings, Finding{
				File:   rel,
				Rule:   RuleBDHooks,
				Msg:    "hook does not carry the beads-managed delegation block — bd events silently skipped",
				Repair: "bd hooks install (re-emits the managed block; keep custom logic outside the markers)",
			})
		}
	}
	return findings
}

// checkReviewGate verifies the pre-push chain end to end (review-gate): live
// hooksPath, executable pre-push, delegation present. Any dead link means
// the gate reads as protection and provides none.
func checkReviewGate(ctx context.Context, dir string) []Finding {
	rel := filepath.Join(hooksDir, "pre-push")

	val, err := hooksPathValue(ctx, dir)
	if err != nil || val != hooksDir {
		return []Finding{{
			File:   rel,
			Rule:   RuleReviewGate,
			Msg:    "pre-push gate unreachable — core.hooksPath does not point at " + hooksDir,
			Repair: "git config core.hooksPath .githooks",
		}}
	}

	fi, statErr := os.Stat(filepath.Join(dir, rel))
	if statErr != nil || fi.Mode()&0o111 == 0 {
		return []Finding{{
			File:   rel,
			Rule:   RuleReviewGate,
			Msg:    "pre-push missing or not executable — nothing gates a push from this machine",
			Repair: "bd hooks install && chmod +x " + rel,
		}}
	}

	data, readErr := os.ReadFile(filepath.Join(dir, rel))
	if readErr != nil || !bytes.Contains(data, []byte(delegationMarker)) {
		return []Finding{{
			File:   rel,
			Rule:   RuleReviewGate,
			Msg:    "pre-push does not delegate to bd — the push gate chain is broken at the last link",
			Repair: "bd hooks install (re-emits the managed delegation block)",
		}}
	}
	return nil
}

// checkBDLive verifies bd's live config: sync.remote configured
// (dolt-remote) and all three keys matching the tracked declaration
// (bd-config-live). One `bd config list` exec — parallel bd invocations
// contend on the embedded Dolt lock (cfm-1e1.2 dead end).
func checkBDLive(ctx context.Context, dir string) []Finding {
	if _, err := exec.LookPath("bd"); err != nil {
		return []Finding{{
			File:   ".beads",
			Rule:   RuleBDConfigLive,
			Msg:    "bd not on PATH — live config unverifiable on this machine",
			Repair: "install beads (bd), then bd init/hydrate this repo",
		}}
	}

	live, err := bdConfigList(ctx, dir)
	if err != nil {
		return []Finding{{
			File:   ".beads",
			Rule:   RuleBDConfigLive,
			Msg:    fmt.Sprintf("bd config list failed (%v) — no live beads store on this machine", err),
			Repair: "bd init --prefix <p> (or hydrate), then bd config set the declared keys",
		}}
	}

	var findings []Finding
	if live["sync.remote"] == "" {
		findings = append(findings, Finding{
			File:   ".beads",
			Rule:   RuleDoltRemote,
			Msg:    "live sync.remote unset — bead history on this machine has no off-machine path",
			Repair: "bd config set sync.remote <declared value from " + bdConfigFile + ">",
		})
	}

	declared := declaredBDConfig(dir)
	for _, k := range bdKeys {
		want := declared[k.key]
		if want == "" {
			continue // undeclared is Surface 1's finding, not a live mismatch
		}
		if got := live[k.key]; got != want {
			findings = append(findings, Finding{
				File:   ".beads",
				Rule:   RuleBDConfigLive,
				Msg:    fmt.Sprintf("live %s is %q, declaration says %q — the two homes have drifted", k.key, got, want),
				Repair: fmt.Sprintf("bd config set %s %q (or fix %s if the live value is right)", k.key, want, bdConfigFile),
			})
		}
	}
	return findings
}

// bdConfigList runs one `bd config list` and parses its "key = value" lines.
func bdConfigList(ctx context.Context, dir string) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(ctx, bdTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bd", "config", "list")
	cmd.Dir = dir
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	values := make(map[string]string)
	for line := range strings.SplitSeq(stdout.String(), "\n") {
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.TrimSpace(val)
	}
	return values, nil
}
