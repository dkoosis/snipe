package checks

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// workflow is the subset of a GitHub Actions file conform-to-sdlc reads. yaml.v3
// resolves a plain `on` key as the string "on" (YAML 1.2), so no key
// gymnastics are needed.
type workflow struct {
	On   yaml.Node               `yaml:"on"`
	Jobs map[string]*workflowJob `yaml:"jobs"`
}

type workflowJob struct {
	Needs yaml.Node `yaml:"needs"` // a job name or a list of them
	If    string    `yaml:"if"`
	Steps []struct {
		If  string `yaml:"if"`
		Run string `yaml:"run"`
	} `yaml:"steps"`
}

// ciGateFile is the workflow that must run `make check`. The rule reads it;
// the scaffold renderer writes it.
const ciGateFile = ".github/workflows/check.yml"

// docsOnlyPaths is the one home of the docs-only path set (ci-docs-skip): a
// pull request whose every changed path matches one of these may skip the
// gate. Each entry is one alternative of the awk regex the detect job runs
// against a changed path; renderDocsOnlyRegex builds that regex from here, and
// checkCIDocsSkip refuses any alternative not listed. A repo may drop entries
// (narrowing — more paths count as code); it may not add them.
var docsOnlyPaths = []string{
	`\.md$`,
	`^docs\/`,
	`^\.beads\/`,
	`^\.claude\/`,
	`^\.gitignore$`,
	`^LICENSE$`,
}

// renderDocsOnlyRegex is the awk regex literal over docsOnlyPaths.
func renderDocsOnlyRegex() string {
	return "/(" + strings.Join(docsOnlyPaths, "|") + ")/"
}

// docsOnlyMatch finds the ignore set in a detect job's run script: the
// parenthesized alternation an awk `$2 !~ /(…)/` tests each changed path
// against. trixi's check.yml is the reference shape.
var docsOnlyMatch = regexp.MustCompile(`\$2\s*!~\s*/\((.*?)\)/`)

// checkCIDocsSkip verifies check.yml skips the gate for a docs-only pull
// request without dropping the required check context (ci-docs-skip): the
// workflow always triggers, a detect job decides docs-only, and the make check
// step waits on that job's output. An absent workflow is ci-gate's finding.
func checkCIDocsSkip(dir string) []Finding {
	const file = ciGateFile
	wf, findings := loadWorkflow(dir, file, RuleCIDocsSkip, "", "")
	if wf == nil {
		return findings
	}

	for _, event := range filteredTriggers(&wf.On) {
		findings = append(findings, Finding{
			File:   file,
			Rule:   RuleCIDocsSkip,
			Msg:    fmt.Sprintf("on.%s carries a workflow-level path filter — a skipped workflow never reports the required check context, so a docs-only PR cannot merge", event),
			Repair: "delete the paths/paths-ignore key; decide docs-only in a detect job instead (see `conform-to-sdlc --fix` on a repo with no check.yml for the shape)",
		})
	}

	guarded, widened := docsSkipGuard(wf)
	for _, w := range widened {
		findings = append(findings, Finding{
			File:   file,
			Rule:   RuleCIDocsSkip,
			Msg:    fmt.Sprintf("detect job %q widens the docs-only set with %q — a repo may narrow conform-to-sdlc's set, never widen it", w.job, w.alt),
			Repair: fmt.Sprintf("remove %q from the job's awk regex; conform-to-sdlc's set is %s", w.alt, renderDocsOnlyRegex()),
		})
	}
	if !guarded {
		findings = append(findings, Finding{
			File:   file,
			Rule:   RuleCIDocsSkip,
			Msg:    "no job decides docs-only before make check runs — a docs-only PR pays the full gate",
			Repair: "mv " + file + " check.yml.old && conform-to-sdlc --fix, then port the old file's extra steps behind the detect guard",
		})
	}
	return findings
}

type widening struct{ job, alt string }

// docsSkipGuard reports whether some make check step waits on a needed job
// that carries a docs-only regex, and every alternative in such a regex that
// conform-to-sdlc's docsOnlyPaths does not list.
func docsSkipGuard(wf *workflow) (guarded bool, widened []widening) {
	for _, need := range gateGuards(wf) {
		alts := detectSets(wf.Jobs[need])
		if len(alts) > 0 {
			guarded = true
		}
		for _, alt := range alts {
			if !slices.Contains(docsOnlyPaths, alt) {
				widened = append(widened, widening{need, alt})
			}
		}
	}
	return guarded, widened
}

// gateGuards names each needed job whose output a make check step (or its
// job) waits on.
func gateGuards(wf *workflow) []string {
	var names []string
	for _, job := range wf.Jobs {
		for _, step := range job.Steps {
			if !strings.Contains(step.Run, "make check") {
				continue
			}
			for _, need := range nodeStrings(&job.Needs) {
				ref := "needs." + need + ".outputs."
				if strings.Contains(step.If, ref) || strings.Contains(job.If, ref) {
					names = append(names, need)
				}
			}
		}
	}
	return names
}

// detectSets returns every docs-only alternative a job's run steps test
// changed paths against.
func detectSets(job *workflowJob) []string {
	if job == nil {
		return nil
	}
	var alts []string
	for _, step := range job.Steps {
		for _, m := range docsOnlyMatch.FindAllStringSubmatch(step.Run, -1) {
			alts = append(alts, strings.Split(m[1], "|")...)
		}
	}
	return alts
}

// filteredTriggers names each event in `on:` that carries paths or
// paths-ignore.
func filteredTriggers(node *yaml.Node) []string {
	var events []string
	if node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		cfg := node.Content[i+1]
		if cfg.Kind != yaml.MappingNode {
			continue
		}
		for j := 0; j+1 < len(cfg.Content); j += 2 {
			if k := cfg.Content[j].Value; k == "paths" || k == "paths-ignore" {
				events = append(events, node.Content[i].Value+"."+k)
			}
		}
	}
	return events
}

// nodeStrings flattens a scalar-or-sequence node into its string values.
func nodeStrings(node *yaml.Node) []string {
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Value != "" {
			return []string{node.Value}
		}
	case yaml.SequenceNode:
		out := make([]string, 0, len(node.Content))
		for _, c := range node.Content {
			out = append(out, c.Value)
		}
		return out
	default: // needs is never a mapping or alias
	}
	return nil
}

// gateInYAML matches a run step re-implementing what a make verb owns. CI =
// `make check` (+ further make verbs); the sharpest fleet divergence was CI
// running its own vet/test YAML while never running golangci-lint at all.
var gateInYAML = regexp.MustCompile(`(?m)^\s*(go (vet|test|build)\b|golangci-lint\s+run\b)`)

// checkCIGate verifies .github/workflows/check.yml calls make check and
// re-implements none of the gate in YAML (ci-gate).
func checkCIGate(dir string) []Finding {
	const file = ciGateFile
	wf, findings := loadWorkflow(dir, file, RuleCIGate,
		"no check workflow — nothing gates a PR",
		"add .github/workflows/check.yml with a step running `make check`")
	if wf == nil {
		return findings
	}

	callsMake := false
	for _, job := range wf.Jobs {
		for _, step := range job.Steps {
			if strings.Contains(step.Run, "make check") {
				callsMake = true
			}
			if m := gateInYAML.FindString(step.Run); m != "" {
				findings = append(findings, Finding{
					File:   file,
					Rule:   RuleCIGate,
					Msg:    fmt.Sprintf("CI re-implements the gate in YAML (%q) — CI and local drift the moment they are two definitions", strings.TrimSpace(m)),
					Repair: "delete the step; make check already owns it",
				})
			}
		}
	}
	if !callsMake {
		findings = append(findings, Finding{
			File:   file,
			Rule:   RuleCIGate,
			Msg:    "no step runs `make check` — CI must run the same gate a developer runs",
			Repair: "add a step: run: make check",
		})
	}
	return findings
}

// checkCodexShape verifies codex-review.yml, when present, fires on
// issue_comment and never pull_request (codex-workflow). An auto-fire
// pull_request trigger is surprise OpenAI spend on every PR.
func checkCodexShape(dir string) []Finding {
	const file = ".github/workflows/codex-review.yml"
	if _, err := os.Stat(filepath.Join(dir, file)); err != nil {
		return nil // the codex workflow is optional; only its shape is contractual
	}
	wf, findings := loadWorkflow(dir, file, RuleCodexShape, "", "")
	if wf == nil {
		return findings
	}

	triggers := triggerNames(&wf.On)
	if !triggers["issue_comment"] {
		findings = append(findings, Finding{
			File:   file,
			Rule:   RuleCodexShape,
			Msg:    "codex-review must be on-demand: triggered by an issue_comment (`@codex review`), not automatic",
			Repair: "set on.issue_comment.types: [created]",
		})
	}
	if triggers["pull_request"] || triggers["pull_request_target"] {
		findings = append(findings, Finding{
			File:   file,
			Rule:   RuleCodexShape,
			Msg:    "codex-review fires on pull_request — auto-fire is surprise OpenAI spend on every PR",
			Repair: "remove the pull_request trigger; keep issue_comment only",
		})
	}
	return findings
}

// loadWorkflow reads and parses one workflow file. A missing file returns
// the supplied missing-finding (or nothing if msg is empty); a parse error
// is always a finding.
func loadWorkflow(dir, file, rule, missingMsg, missingRepair string) (*workflow, []Finding) {
	data, err := os.ReadFile(filepath.Join(dir, file))
	if err != nil {
		if missingMsg == "" {
			return nil, nil
		}
		return nil, []Finding{{File: file, Rule: rule, Msg: missingMsg, Repair: missingRepair}}
	}
	var wf workflow
	if err := yaml.Unmarshal(data, &wf); err != nil {
		return nil, []Finding{{
			File:   file,
			Rule:   rule,
			Msg:    fmt.Sprintf("unparseable YAML: %v", err),
			Repair: "fix the workflow YAML",
		}}
	}
	return &wf, nil
}

// triggerNames flattens a workflow `on:` node — string, sequence, or map —
// into the set of event names.
func triggerNames(node *yaml.Node) map[string]bool {
	names := make(map[string]bool)
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Value != "" {
			names[node.Value] = true
		}
	case yaml.SequenceNode:
		for _, c := range node.Content {
			names[c.Value] = true
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			names[node.Content[i].Value] = true
		}
	default: // DocumentNode/AliasNode never appear at a workflow's `on` key
	}
	return names
}
