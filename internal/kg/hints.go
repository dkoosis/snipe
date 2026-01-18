// Package kg provides integration with Orca knowledge graph for contextual hints.
package kg

import (
	"os"
	"os/exec"
	"strings"
)

// Config specifies what to query for hints.
type Config struct {
	File    string // File path (relative)
	Symbol  string // Symbol name
	Package string // Package path
}

// Hint represents a knowledge graph hint.
type Hint struct {
	ID       string
	Kind     string // trap, pattern, map, etc.
	Severity string // h, m, l for traps
	Summary  string
}

// GetHints queries the Orca KG for hints related to the given config.
// Returns empty slice if Orca is not available or no hints found.
func GetHints(cfg Config) []Hint {
	// Check if orca CLI is available
	orcaPath, err := exec.LookPath("orca")
	if err != nil {
		return nil
	}

	// Build query: look for traps/patterns related to file or symbol
	var hints []Hint

	// Query for file-anchored hints
	if cfg.File != "" {
		fileHints := queryOrcaHints(orcaPath, "file:"+cfg.File)
		hints = append(hints, fileHints...)
	}

	// Query for symbol-specific hints
	if cfg.Symbol != "" {
		symHints := queryOrcaHints(orcaPath, "sym:"+cfg.Symbol)
		hints = append(hints, symHints...)
	}

	// Query for package-related hints
	if cfg.Package != "" {
		// Extract last component of package path
		parts := strings.Split(cfg.Package, "/")
		if len(parts) > 0 {
			pkgHints := queryOrcaHints(orcaPath, "pkg:"+parts[len(parts)-1])
			hints = append(hints, pkgHints...)
		}
	}

	return hints
}

// queryOrcaHints runs orca search_nugs and parses results.
func queryOrcaHints(orcaPath, query string) []Hint {
	// Use environment to pass query to orca
	// Format: orca search_nugs --query <query> --format json
	cmd := exec.Command(orcaPath, "search_nugs", "--query", query, "--limit", "3", "--format", "oneline")
	cmd.Env = append(os.Environ(), "ORCA_QUIET=1")

	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	return parseOrcaOutput(string(output))
}

// parseOrcaOutput parses orca search_nugs output.
// Expected format: ID | KIND | SUMMARY (one per line)
func parseOrcaOutput(output string) []Hint {
	var hints []Hint
	lines := strings.Split(strings.TrimSpace(output), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Parse pipe-separated format: id | kind | summary
		parts := strings.SplitN(line, "|", 3)
		if len(parts) < 3 {
			continue
		}

		id := strings.TrimSpace(parts[0])
		kind := strings.TrimSpace(parts[1])
		summary := strings.TrimSpace(parts[2])

		// Extract severity from kind if present (e.g., "trap:h")
		var severity string
		if idx := strings.Index(kind, ":"); idx >= 0 {
			severity = kind[idx+1:]
			kind = kind[:idx]
		}

		hints = append(hints, Hint{
			ID:       id,
			Kind:     kind,
			Severity: severity,
			Summary:  summary,
		})
	}

	return hints
}
