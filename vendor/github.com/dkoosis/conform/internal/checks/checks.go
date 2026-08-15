// Package checks implements Surface 1: the in-repo file checks that run bare
// (`conform`) inside a repository, every `make check`, hard-fail.
//
// Every finding names the file, the rule id, and a repair command. There is no
// soft-fail: a rule too noisy to hard-fail gets deleted, not warned.
//
// Rule ids are the shared vocabulary between this checker and the values
// file's exceptions (see internal/values): a repo excepts a rule by listing
// its id in conform.json with a reason. Ids are kebab-case and stable.
package checks

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"

	"github.com/dkoosis/conform/internal/values"
)

// Surface-1 rule ids. Other surfaces reserve their own ids in the same
// vocabulary (e.g. `no-git-ops`, honored by --local for repos like loto that
// declare git operations out of scope).
const (
	RuleValuesFile   = "values-file"       // conform.json present and valid
	RuleMakefileVerb = "makefile-verbs"    // four-verb contract + prereq composition
	RuleMakefileDocs = "makefile-docs"     // every target carries a ## doc comment
	RuleLintFloor    = "lint-floor"        // .golangci.yml carries the 6-linter baseline floor
	RuleLintContext  = "lint-contextcheck" // contextcheck must stay demoted
	RuleLintPin      = "lint-pin"          // exactly one golangci-lint pin location
	RuleCIGate       = "ci-gate"           // CI calls make check; no gate re-implemented in YAML
	RuleCodexShape   = "codex-workflow"    // codex-review fires on issue_comment, never pull_request
	RuleRetiredFiles = "retired-files"     // files folded into conform are gone
	RuleBDConfig     = "bd-config"         // bd config keys present
	RuleHooksShape   = "hooks-shape"       // shape B: tracked .githooks
)

// Finding is one contract violation: which file, which rule, what to run.
type Finding struct {
	File   string // repo-relative path
	Rule   string // rule id (stable, kebab-case)
	Msg    string
	Repair string // the command or edit that fixes it
}

func (f Finding) String() string {
	return fmt.Sprintf("%s: %s: %s\n    fix: %s", f.File, f.Rule, f.Msg, f.Repair)
}

// Run executes all Surface-1 checks against the repo rooted at dir and
// returns the findings, with the repo's declared exceptions (conform.json)
// filtered out. A missing or invalid values file is itself a finding.
func Run(dir string) []Finding {
	vals, findings := loadValues(dir)

	findings = append(findings, checkMakefile(dir, vals.Profile)...)
	findings = append(findings, checkLintFloor(dir)...)
	findings = append(findings, checkLintPin(dir)...)
	findings = append(findings, checkCIGate(dir)...)
	findings = append(findings, checkCodexShape(dir)...)
	findings = append(findings, checkRetiredFiles(dir)...)
	findings = append(findings, checkBDConfig(dir)...)
	findings = append(findings, checkHooksShape(dir)...)

	findings = applyExceptions(findings, vals)
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Rule < findings[j].Rule
	})
	return findings
}

// loadValues reads conform.json. Absent or invalid, the checks still run —
// under the default tool profile with no exceptions — and the values problem
// is reported as a finding of its own.
func loadValues(dir string) (values.Values, []Finding) {
	fallback := values.Values{Profile: values.ProfileTool}
	path := filepath.Join(dir, "conform.json")
	v, err := values.Load(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fallback, []Finding{{
				File:   "conform.json",
				Rule:   RuleValuesFile,
				Msg:    "values file missing — conform needs the repo's profile (tool|lib) and declared exceptions",
				Repair: `create conform.json: {"profile": "tool", "exceptions": []}`,
			}}
		}
		return fallback, []Finding{{
			File:   "conform.json",
			Rule:   RuleValuesFile,
			Msg:    err.Error(),
			Repair: "fix conform.json: profile tool|lib; every exception needs rule + reason",
		}}
	}
	return *v, nil
}

// applyExceptions drops findings whose rule id the repo has excepted with a
// reason. The exception mechanism is the only sanctioned suppression — there
// is no severity dial and no warn level.
func applyExceptions(findings []Finding, vals values.Values) []Finding {
	if len(vals.Exceptions) == 0 {
		return findings
	}
	excepted := make(map[string]bool, len(vals.Exceptions))
	for _, e := range vals.Exceptions {
		excepted[e.Rule] = true
		if e.Rule == RuleNoGitOps {
			// One word excepts the whole git-hook family (loto: a repo that
			// performs no git operations by design has no hook chain to keep
			// alive).
			for _, r := range noGitOpsFamily {
				excepted[r] = true
			}
		}
	}
	kept := findings[:0]
	for _, f := range findings {
		if !excepted[f.Rule] {
			kept = append(kept, f)
		}
	}
	return kept
}
