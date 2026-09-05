package checks

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// workflow is the subset of a GitHub Actions file conform reads. yaml.v3
// resolves a plain `on` key as the string "on" (YAML 1.2), so no key
// gymnastics are needed.
type workflow struct {
	On   yaml.Node `yaml:"on"`
	Jobs map[string]struct {
		Steps []struct {
			Run string `yaml:"run"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

// ciGateFile is the workflow that must run `make check`. The rule reads it;
// the scaffold renderer writes it.
const ciGateFile = ".github/workflows/check.yml"

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
