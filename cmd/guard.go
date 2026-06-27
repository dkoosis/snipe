package cmd

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
)

// errGuardFailed is the sentinel returned on any operator/spec error. guardCmd
// sets SilenceErrors+SilenceUsage so the structured message (already written via
// the output writer) is not duplicated; the sentinel only drives a non-zero exit.
var errGuardFailed = errors.New("guard: spec or index error")

// defaultGuardSpec is the spec read when no path arg is given.
const defaultGuardSpec = ".snipe/boundaries.yml"

// guardFail writes a structured operator error and returns the sentinel so the
// process exits non-zero (a gate must fail closed on spec/index problems).
func guardFail(w *output.Writer, msg string) error {
	_ = w.WriteError("guard", &output.Error{Code: output.ErrInternal, Message: msg})
	return errGuardFailed
}

// guardSpec is the on-disk boundary policy.
type guardSpec struct {
	Version int         `yaml:"version"`
	Rules   []guardRule `yaml:"rules"`
}

// guardRule denies references from one package set to another. Kinds, when set,
// restrict the rule to those ref kinds (func|method|type|...); empty = all.
// Allow lists file-path suffixes that are exempted (ratchet for known sites).
type guardRule struct {
	Name         string   `yaml:"name"`
	From         string   `yaml:"from"`
	To           string   `yaml:"to"`
	Desc         string   `yaml:"desc"`
	Kinds        []string `yaml:"kinds"`
	Allow        []string `yaml:"allow"`
	IncludeTests bool     `yaml:"include_tests"` // default false: _test.go sites are exempt
}

// guardViolation is one forbidden reference site.
type guardViolation struct {
	Rule    string `json:"rule"`
	Desc    string `json:"desc"`
	Symbol  string `json:"symbol"`
	Kind    string `json:"kind"`
	FromPkg string `json:"from_pkg"`
	ToPkg   string `json:"to_pkg"`
	File    string `json:"file"`
	Line    int    `json:"line"`
}

func runGuard(args []string) error {
	start := time.Now()
	w := output.NewWriter(os.Stdout, GetOutputFormat())

	specPath := defaultGuardSpec
	if len(args) == 1 {
		specPath = args[0]
	}

	data, err := os.ReadFile(specPath)
	if err != nil {
		return guardFail(w, "read spec: "+err.Error())
	}
	var spec guardSpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return guardFail(w, "parse spec: "+err.Error())
	}
	if len(spec.Rules) == 0 {
		return guardFail(w, "spec has no rules: "+specPath)
	}

	s, dir, err := OpenStore(w, "guard")
	if err != nil {
		return err
	}
	defer s.Close()

	universe, err := allPkgPaths(s.DB())
	if err != nil {
		return guardFail(w, err.Error())
	}

	violations, err := evalGuard(s.DB(), universe, spec.Rules)
	if err != nil {
		return guardFail(w, err.Error())
	}

	sort.Slice(violations, func(i, j int) bool {
		a, b := violations[i], violations[j]
		if a.Rule != b.Rule {
			return a.Rule < b.Rule
		}
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Line < b.Line
	})

	if err := writeGuard(dir, specPath, spec.Rules, violations, start); err != nil {
		return err
	}
	if len(violations) > 0 {
		// Non-zero exit for CI gating. Message is short; detail is in the report.
		return fmt.Errorf("guard: %d boundary violation(s)", len(violations))
	}
	return nil
}

// evalGuard resolves each rule and collects every forbidden reference site.
// An unmatched from/to pattern is a hard error so rules never silently no-op.
func evalGuard(db *sql.DB, universe []string, rules []guardRule) ([]guardViolation, error) {
	var out []guardViolation
	for _, r := range rules {
		if r.From == "" || r.To == "" {
			return nil, fmt.Errorf("rule %q: from and to are required", r.Name)
		}
		setFrom := query.MatchPackagePatterns(universe, []string{r.From})
		setTo := query.MatchPackagePatterns(universe, []string{r.To})
		if len(setFrom) == 0 {
			return nil, fmt.Errorf("rule %q: from pattern matched no packages: %q", r.Name, r.From)
		}
		if len(setTo) == 0 {
			return nil, fmt.Errorf("rule %q: to pattern matched no packages: %q", r.Name, r.To)
		}

		report, err := query.FindBoundaryCrossings(db, setFrom, setTo)
		if err != nil {
			return nil, fmt.Errorf("rule %q: %w", r.Name, err)
		}
		if err := query.PopulateBoundaryLocations(db, report); err != nil {
			return nil, fmt.Errorf("rule %q: locations: %w", r.Name, err)
		}

		kinds := kindSet(r.Kinds)
		for _, ref := range report.AToB { // from -> to is the forbidden direction
			if len(kinds) > 0 && !kinds[ref.Kind] {
				continue
			}
			out = append(out, refViolations(r, ref)...)
		}
	}
	return out, nil
}

// refViolations expands one crossing into per-site violations, applying the
// rule's allow-list (file-suffix exemptions). A crossing with no recorded
// locations still yields one violation (file unknown) so it is never hidden.
func refViolations(r guardRule, ref query.BoundaryRef) []guardViolation {
	mk := func(file string, line int) guardViolation {
		return guardViolation{
			Rule: r.Name, Desc: r.Desc, Symbol: ref.Symbol, Kind: ref.Kind,
			FromPkg: ref.SourcePkg, ToPkg: ref.TargetPkg, File: file, Line: line,
		}
	}
	if len(ref.Locations) == 0 {
		return []guardViolation{mk("", 0)}
	}
	var out []guardViolation
	for _, loc := range ref.Locations {
		if !r.IncludeTests && strings.HasSuffix(loc.File, "_test.go") {
			continue
		}
		if allowed(r.Allow, loc.File) {
			continue
		}
		out = append(out, mk(loc.File, loc.Line))
	}
	return out
}

func kindSet(kinds []string) map[string]bool {
	if len(kinds) == 0 {
		return nil
	}
	m := make(map[string]bool, len(kinds))
	for _, k := range kinds {
		m[strings.TrimSpace(k)] = true
	}
	return m
}

// allowed reports whether file matches any allow-list suffix. The suffix must
// align on a path boundary, so "db.go" matches "pkg/db.go" but not "validb.go".
func allowed(allow []string, file string) bool {
	for _, a := range allow {
		if a == "" {
			continue
		}
		if file == a || strings.HasSuffix(file, "/"+a) {
			return true
		}
	}
	return false
}

func writeGuard(dir, specPath string, rules []guardRule, v []guardViolation, start time.Time) error {
	if GetOutputFormat() == output.OutputJSON {
		resp := output.Response[guardViolation]{
			Protocol: output.ProtocolVersion,
			Ok:       len(v) == 0,
			Results:  v,
			Meta: output.Meta{
				Command:  "guard",
				Query:    map[string]string{"spec": specPath},
				RepoRoot: dir,
				Ms:       time.Since(start).Milliseconds(),
				Total:    len(v),
			},
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	}

	var b strings.Builder
	if len(v) == 0 {
		fmt.Fprintf(&b, "guard · %d rule(s) · ✓ no violations\n", len(rules))
		_, err := os.Stdout.WriteString(b.String())
		return err
	}
	fmt.Fprintf(&b, "guard · %d rule(s) · ✗ %d violation(s)\n", len(rules), len(v))
	var lastRule string
	for _, x := range v {
		if x.Rule != lastRule {
			fmt.Fprintf(&b, "\n  [%s] %s\n", x.Rule, x.Desc)
			lastRule = x.Rule
		}
		loc := x.File
		if x.Line > 0 {
			loc = fmt.Sprintf("%s:%d", x.File, x.Line)
		}
		if loc == "" {
			loc = "(location unknown)"
		}
		fmt.Fprintf(&b, "    %s -> %s.%s [%s]\n      %s\n", x.FromPkg, x.ToPkg, x.Symbol, x.Kind, loc)
	}
	_, err := os.Stdout.WriteString(b.String())
	return err
}
