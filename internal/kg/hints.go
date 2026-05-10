// Package kg provides integration with Orca knowledge graph for contextual hints.
package kg

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

// orcaQueryTimeout is the per-call deadline for an `orca search_nugs` subprocess.
// KG hints are best-effort decoration; never block the primary command on a hung child.
const orcaQueryTimeout = 2 * time.Second

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

// IsAvailable reports whether the Orca KG bridge is usable in this process —
// i.e., whether an `orca` binary is resolvable via PATH. Callers that pass
// --kg-hints can distinguish "no hints" from "KG unreachable" using this.
func IsAvailable() bool {
	_, err := exec.LookPath("orca")
	return err == nil
}

// GetHints queries the Orca KG for hints related to the given config.
// Returns nil if Orca is not available or no hints found.
// ctx cancellation aborts in-flight orca subprocesses; each call also has a
// per-query timeout so a hung orca never blocks snipe forever.
func GetHints(ctx context.Context, cfg Config) []Hint {
	orcaPath, err := exec.LookPath("orca")
	if err != nil {
		return nil
	}

	env := append(os.Environ(), "ORCA_QUIET=1")

	var queries []string
	if cfg.File != "" {
		queries = append(queries, "file:"+cfg.File)
	}
	if cfg.Symbol != "" {
		queries = append(queries, "sym:"+cfg.Symbol)
	}
	if cfg.Package != "" {
		pkg := cfg.Package
		if idx := strings.LastIndex(pkg, "/"); idx >= 0 {
			pkg = pkg[idx+1:]
		}
		queries = append(queries, "pkg:"+pkg)
	}

	var hints []Hint
	seen := make(map[string]struct{})
	for _, q := range queries {
		if ctx.Err() != nil {
			break
		}
		for _, h := range queryOrcaHints(ctx, orcaPath, env, q) {
			if _, dup := seen[h.ID]; dup {
				continue
			}
			seen[h.ID] = struct{}{}
			hints = append(hints, h)
		}
	}
	return hints
}

func queryOrcaHints(ctx context.Context, orcaPath string, env []string, query string) []Hint {
	callCtx, cancel := context.WithTimeout(ctx, orcaQueryTimeout)
	defer cancel()

	cmd := exec.CommandContext(callCtx, orcaPath, "search_nugs", "--query", query, "--limit", "3", "--format", "oneline")
	cmd.Env = env

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
