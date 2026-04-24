package context

import (
	"fmt"
	"math"
	"strings"
)

const empty = "∅"

// FormatText renders a BootContext as compact claudish text.
// Optimized for Claude consumption: tables, fragments, minimal noise.
func FormatText(bc *BootContext) string {
	var b strings.Builder

	// Header
	fmt.Fprintf(&b, "# %s", bc.Project)
	if bc.Commit != "" {
		fmt.Fprintf(&b, " (%s)", bc.Commit)
	}
	b.WriteByte('\n')
	if bc.Purpose != "" {
		b.WriteString(bc.Purpose)
		b.WriteByte('\n')
	}

	// Build info
	if bi := bc.BuildInfo; bi != nil {
		b.WriteString("\n## build\n")
		fmt.Fprintf(&b, "%s | test: %s\n", bi.Build, bi.Test)
		if len(bi.Targets) > 0 {
			fmt.Fprintf(&b, "targets: %s\n", strings.Join(bi.Targets, " "))
		}
		for _, ci := range bi.CI {
			fmt.Fprintf(&b, "ci: %s → %s\n", ci.System, ci.File)
		}
	}

	// Entry points
	if len(bc.EntryPoints) > 0 {
		b.WriteString("\n## entry\n")
		for _, ep := range bc.EntryPoints {
			b.WriteString(ep)
			b.WriteByte('\n')
		}
	}

	// Key symbols
	if len(bc.KeySymbols) > 0 {
		b.WriteString("\n## key symbols\n")
		for _, s := range bc.KeySymbols {
			loc := fmt.Sprintf("%s:%d", s.File, s.Line)
			if s.Purpose != "" {
				fmt.Fprintf(&b, "%s (%s) %s — %s\n", s.Name, loc, s.Role, s.Purpose)
			} else {
				fmt.Fprintf(&b, "%s (%s) %s\n", s.Name, loc, s.Role)
			}
		}
	}

	// Boot views
	if bv := bc.BootViews; bv != nil {
		formatCommands(&b, bv.Commands)
		formatFlows(&b, bv.PrimaryFlows)
		formatBoundaries(&b, bv.ChangeBoundaries)
		formatInterfaces(&b, bv.InterfaceMap)
	}

	// Packages
	if len(bc.Packages) > 0 {
		b.WriteString("\n## packages\n")
		for _, p := range bc.Packages {
			if p.Purpose != "" {
				fmt.Fprintf(&b, "%s: %s\n", p.Name, p.Purpose)
			} else {
				fmt.Fprintf(&b, "%s: %s\n", p.Name, empty)
			}
		}
	}

	// Dep DAG
	if dag := bc.DepDAG; dag != nil && len(dag.Edges) > 0 {
		b.WriteString("\n## deps\n")
		for _, e := range dag.Edges {
			fmt.Fprintf(&b, "%s → %s\n", e.From, strings.Join(e.To, " "))
		}
	}

	// Conventions
	if conv := bc.Conventions; conv != nil {
		formatConventions(&b, conv)
	}

	// DB schemas
	formatDBSchemas(&b, bc.DBSchemas)

	// Active work
	if aw := bc.ActiveWork; aw != nil {
		b.WriteString("\n## active work\n")
		if aw.Branch != "" {
			fmt.Fprintf(&b, "branch: %s\n", aw.Branch)
		}
		for _, s := range aw.RecentSymbols {
			fmt.Fprintf(&b, "  %s (%s:%d)\n", s.Name, s.File, s.Line)
		}
	}

	return b.String()
}

// formatDBSchemas emits one "## db schema" section per detected schema.
// Source and name are shown in the header; DDL follows in a fenced sql block.
func formatDBSchemas(b *strings.Builder, schemas []DBSchema) {
	for _, s := range schemas {
		fmt.Fprintf(b, "\n## db schema: %s (%s)\n", s.Name, s.Source)
		b.WriteString("```sql\n")
		b.WriteString(s.DDL)
		if !strings.HasSuffix(s.DDL, "\n") {
			b.WriteByte('\n')
		}
		b.WriteString("```\n")
	}
}

func formatCommands(b *strings.Builder, ct *CommandTable) {
	if ct == nil || len(ct.Commands) == 0 {
		return
	}
	b.WriteString("\n## commands\n")
	if len(ct.BoilerplatePattern) > 0 {
		fmt.Fprintf(b, "‡ boilerplate: %s\n", strings.Join(ct.BoilerplatePattern, ", "))
	}
	for _, c := range ct.Commands {
		if c.Purpose == "" {
			continue
		}
		if len(c.Callees) > 0 {
			fmt.Fprintf(b, "%s — %s → %s\n", c.Name, c.Purpose, strings.Join(c.Callees, ", "))
		} else {
			fmt.Fprintf(b, "%s — %s\n", c.Name, c.Purpose)
		}
	}
}

func formatFlows(b *strings.Builder, flows []string) {
	if len(flows) == 0 {
		return
	}
	b.WriteString("\n## flows\n")
	for _, f := range flows {
		b.WriteString(f)
		b.WriteByte('\n')
	}
}

func formatBoundaries(b *strings.Builder, boundaries map[string][]string) {
	if len(boundaries) == 0 {
		return
	}
	b.WriteString("\n## boundaries\n")
	for concern, syms := range boundaries {
		fmt.Fprintf(b, "%s: %s\n", concern, strings.Join(syms, ", "))
	}
}

func formatInterfaces(b *strings.Builder, ifaces []InterfaceEntry) {
	if len(ifaces) == 0 {
		return
	}
	b.WriteString("\n## interfaces\n")
	for _, i := range ifaces {
		loc := fmt.Sprintf("%s:%d", i.File, i.Line)
		var methods string
		if len(i.Methods) > 0 {
			shown := i.Methods[:min(3, len(i.Methods))]
			methods = strings.Join(shown, ", ")
			if len(i.Methods) > 3 {
				methods += fmt.Sprintf(" +%d", len(i.Methods)-3)
			}
		}
		if len(i.Implementors) > 0 {
			names := make([]string, len(i.Implementors))
			for j, imp := range i.Implementors {
				names[j] = imp.Name
			}
			if methods != "" {
				fmt.Fprintf(b, "%s (%s): %s → %s\n", i.Interface, loc, methods, strings.Join(names, ", "))
			} else {
				fmt.Fprintf(b, "%s (%s) → %s\n", i.Interface, loc, strings.Join(names, ", "))
			}
		} else {
			if methods != "" {
				fmt.Fprintf(b, "%s (%s): %s\n", i.Interface, loc, methods)
			} else {
				fmt.Fprintf(b, "%s (%s)\n", i.Interface, loc)
			}
		}
	}
}

// confSym maps confidence string to claudish symbol.
func confSym(confidence string) string {
	switch confidence {
	case "high":
		return "✓"
	case "medium":
		return "≈"
	default:
		return confidence
	}
}

func formatConventions(b *strings.Builder, conv *Conventions) {
	b.WriteString("\n## conventions\n")
	if c := conv.Constructors; c != nil {
		fmt.Fprintf(b, "constructors: %s %d/%d %s\n",
			c.Pattern, c.Total, c.WithError, confSym(c.Confidence))
	}
	if c := conv.Receivers; c != nil {
		pct := int(math.Round(c.PointerPct))
		fmt.Fprintf(b, "receivers: %s %d%%ptr/%d %s\n",
			c.Pattern, pct, c.Total, confSym(c.Confidence))
	}
	if c := conv.Testing; c != nil {
		fmt.Fprintf(b, "testing: %s %df/%dh %s\n",
			c.Pattern, c.TestFiles, c.Helpers, confSym(c.Confidence))
	}
	if c := conv.Interfaces; c != nil {
		fmt.Fprintf(b, "interfaces: %s %d/%d-er %s\n",
			c.Pattern, c.Total, c.ErSuffix, confSym(c.Confidence))
	}
	if c := conv.ErrorHandling; c != nil {
		fmt.Fprintf(b, "errors: %s %d/%d %s\n",
			c.Pattern, c.Sentinels, c.ErrorFuncs, confSym(c.Confidence))
	}
	if c := conv.FileOrg; c != nil {
		fmt.Fprintf(b, "file org: %s %.1ft/f %s\n",
			c.Pattern, c.AvgTypesFile, confSym(c.Confidence))
	}
}
