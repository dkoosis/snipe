package context

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Nugget represents an Orca knowledge graph nugget for save_nug.
type Nugget struct {
	ID   string   `yaml:"id"`
	Kind string   `yaml:"k"`
	Body string   `yaml:"b"`
	Tags []string `yaml:"tags,omitempty"`
}

// ToNuggets converts BootContext to Orca nuggets.
func (b *BootContext) ToNuggets() []Nugget {
	projectSlug := slugify(b.Project)

	// Build the body content
	var body strings.Builder
	fmt.Fprintf(&body, "lang: %s\n", b.Lang)
	if b.BuildInfo != nil {
		fmt.Fprintf(&body, "build: %s\n", b.BuildInfo.Build)
		fmt.Fprintf(&body, "test: %s\n", b.BuildInfo.Test)
	}

	if len(b.EntryPoints) > 0 {
		body.WriteString("entry_points:\n")
		for _, ep := range b.EntryPoints {
			fmt.Fprintf(&body, "  - %s\n", ep)
		}
	}

	if len(b.KeySymbols) > 0 {
		body.WriteString("key_symbols:\n")
		for _, sym := range b.KeySymbols {
			fmt.Fprintf(&body, "  - %s (%s:%d)\n", sym.Name, sym.File, sym.Line)
		}
	}

	if b.Commit != "" {
		fmt.Fprintf(&body, "commit: %s\n", b.Commit)
	}

	return []Nugget{
		{
			ID:   fmt.Sprintf("n:project:%s", projectSlug),
			Kind: "project",
			Body: body.String(),
			Tags: []string{projectSlug, "boot-context"},
		},
	}
}

// ToNuggets converts ProjectContext to Orca nuggets.
func (p *ProjectContext) ToNuggets() []Nugget {
	projectSlug := slugify(p.Project.Name)
	var nugs []Nugget

	// Project overview nugget
	var projBody strings.Builder
	fmt.Fprintf(&projBody, "name: %s\n", p.Project.Name)
	fmt.Fprintf(&projBody, "root: %s\n", p.Project.Root)
	if len(p.Project.Lang) > 0 {
		fmt.Fprintf(&projBody, "lang: %s\n", strings.Join(p.Project.Lang, ", "))
	}
	if p.Project.Build != "" {
		fmt.Fprintf(&projBody, "build: %s\n", p.Project.Build)
	}
	if p.Project.Test != "" {
		fmt.Fprintf(&projBody, "test: %s\n", p.Project.Test)
	}

	nugs = append(nugs, Nugget{
		ID:   fmt.Sprintf("n:project:%s", projectSlug),
		Kind: "project",
		Body: projBody.String(),
		Tags: []string{projectSlug},
	})

	// Architecture nugget
	if len(p.Architecture.Components) > 0 {
		var archBody strings.Builder
		archBody.WriteString("components:\n")
		for _, comp := range p.Architecture.Components {
			fmt.Fprintf(&archBody, "  %s:\n", comp.Name)
			fmt.Fprintf(&archBody, "    purpose: %s\n", comp.Purpose)
			if comp.Entry != "" {
				fmt.Fprintf(&archBody, "    entry: %s\n", comp.Entry)
			}
			if len(comp.KeyFiles) > 0 {
				archBody.WriteString("    key_files:\n")
				for _, f := range comp.KeyFiles {
					fmt.Fprintf(&archBody, "      - %s\n", f)
				}
			}
		}

		if len(p.Architecture.DataFlows) > 0 {
			archBody.WriteString("data_flows:\n")
			for _, df := range p.Architecture.DataFlows {
				fmt.Fprintf(&archBody, "  - %s -> %s", df.From, df.To)
				if df.Via != "" {
					fmt.Fprintf(&archBody, " (%s)", df.Via)
				}
				archBody.WriteString("\n")
			}
		}

		if len(p.Architecture.Boundaries) > 0 {
			archBody.WriteString("boundaries:\n")
			for _, b := range p.Architecture.Boundaries {
				fmt.Fprintf(&archBody, "  %s:\n", b.Package)
				if len(b.Owns) > 0 {
					fmt.Fprintf(&archBody, "    owns: [%s]\n", strings.Join(b.Owns, ", "))
				}
				if len(b.Exports) > 0 {
					fmt.Fprintf(&archBody, "    exports: [%s]\n", strings.Join(b.Exports, ", "))
				}
			}
		}

		nugs = append(nugs, Nugget{
			ID:   fmt.Sprintf("n:map:%s-arch", projectSlug),
			Kind: "map",
			Body: archBody.String(),
			Tags: []string{projectSlug, "architecture"},
		})
	}

	// Key symbols nugget
	if len(p.Symbols.Types) > 0 || len(p.Symbols.Functions) > 0 {
		var symBody strings.Builder
		if len(p.Symbols.Types) > 0 {
			symBody.WriteString("types:\n")
			for _, t := range p.Symbols.Types {
				fmt.Fprintf(&symBody, "  - %s (%s:%d)\n", t.Name, t.File, t.Line)
			}
		}
		if len(p.Symbols.Functions) > 0 {
			symBody.WriteString("functions:\n")
			for _, f := range p.Symbols.Functions {
				fmt.Fprintf(&symBody, "  - %s (%s:%d)\n", f.Name, f.File, f.Line)
			}
		}

		nugs = append(nugs, Nugget{
			ID:   fmt.Sprintf("n:map:%s-symbols", projectSlug),
			Kind: "map",
			Body: symBody.String(),
			Tags: []string{projectSlug, "symbols"},
		})
	}

	return nugs
}

// slugify converts a string to a slug suitable for nugget IDs.
func slugify(s string) string {
	// Get base name if it's a path
	s = filepath.Base(s)
	// Convert to lowercase
	s = strings.ToLower(s)
	// Replace spaces and underscores with hyphens
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	// Remove any characters that aren't alphanumeric or hyphens
	var result strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			result.WriteRune(r)
		}
	}
	return result.String()
}
