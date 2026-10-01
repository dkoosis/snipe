package graphmetrics

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/dkoosis/snipe/internal/gitchurn"
	"github.com/dkoosis/snipe/internal/index"
	"github.com/dkoosis/snipe/internal/store"
)

// computeCycloRollups persists per-package McCabe cyclomatic-complexity
// rollups (sum, p95, max) under the imports graph as cyclo_sum/p95/max;
// `snipe metrics --kind=cyclo` joins them at read time.
// Hot-package threshold: p95 > 10 OR max > 20.
func computeCycloRollups(s *store.Store, log io.Writer, symbols []index.Symbol) error {
	return writeComplexityRollups(s, log, "cyclo", index.PackageCycloRollups(symbols))
}

// computeCognitiveRollups is the cognitive-complexity counterpart, stored as
// cognitive_sum/p95/max for `snipe metrics --kind=cognitive`.
func computeCognitiveRollups(s *store.Store, log io.Writer, symbols []index.Symbol) error {
	return writeComplexityRollups(s, log, "cognitive", index.PackageCognitiveRollups(symbols))
}

// writeComplexityRollups persists a per-package rollup map under the imports
// graph as {prefix}_sum/{prefix}_p95/{prefix}_max in one place, so cyclo and
// cognitive share the storage shape.
func writeComplexityRollups(s *store.Store, log io.Writer, prefix string, rollups map[string]index.CycloRollup) error {
	if len(rollups) == 0 {
		return nil
	}
	t0 := time.Now()
	sum := make(map[string]float64, len(rollups))
	p95 := make(map[string]float64, len(rollups))
	maxv := make(map[string]float64, len(rollups))
	for pkg, r := range rollups {
		sum[pkg] = float64(r.Sum)
		p95[pkg] = r.P95
		maxv[pkg] = float64(r.Max)
	}
	if err := s.WriteGraphMetrics("imports", prefix+"_sum", sum); err != nil {
		return fmt.Errorf("write %s_sum: %w", prefix, err)
	}
	if err := s.WriteGraphMetrics("imports", prefix+"_p95", p95); err != nil {
		return fmt.Errorf("write %s_p95: %w", prefix, err)
	}
	if err := s.WriteGraphMetrics("imports", prefix+"_max", maxv); err != nil {
		return fmt.Errorf("write %s_max: %w", prefix, err)
	}
	fmt.Fprintf(log, "metrics: %s rollups computed for %d packages in %dms\n",
		prefix, len(rollups), time.Since(t0).Milliseconds())
	return nil
}

// computeFileComplexity aggregates per-symbol McCabe and cognitive complexity
// into per-file rollups, stored under a synthetic "files" graph. `snipe
// hotspots` joins these against file_churn (path-keyed) and fan-in to form the
// unified risk view. Test files are excluded to match computeCycloRollups —
// hotspots targets production risk. Paths are relativized to repo_root so they
// line up with the churn and symbols tables.
func computeFileComplexity(s *store.Store, symbols []index.Symbol) error {
	root, err := s.GetMeta("repo_root")
	if err != nil {
		return fmt.Errorf("read repo_root: %w", err)
	}
	sum := make(map[string]float64)
	maxv := make(map[string]float64)
	cog := make(map[string]float64)
	for i := range symbols {
		sym := &symbols[i]
		if sym.Kind != index.KindFunc && sym.Kind != index.KindMethod {
			continue
		}
		if strings.HasSuffix(sym.FilePath, "_test.go") {
			continue
		}
		rel := RelToRoot(root, sym.FilePath)
		c := float64(sym.Cyclo)
		sum[rel] += c
		if c > maxv[rel] {
			maxv[rel] = c
		}
		cog[rel] += float64(sym.Cognitive)
	}
	if len(sum) == 0 {
		return nil
	}
	if err := s.WriteGraphMetrics("files", "cyclo_sum", sum); err != nil {
		return fmt.Errorf("write file cyclo_sum: %w", err)
	}
	if err := s.WriteGraphMetrics("files", "cyclo_max", maxv); err != nil {
		return fmt.Errorf("write file cyclo_max: %w", err)
	}
	if err := s.WriteGraphMetrics("files", "cognitive_sum", cog); err != nil {
		return fmt.Errorf("write file cognitive_sum: %w", err)
	}
	return nil
}

// computeFileFanIn stores per-file blast-radius (distinct caller files)
// under the "files" graph for `snipe hotspots`. FileFanIn keys come straight
// from symbols.file_path (absolute), so they are relativized here to line up
// with the cyclo/churn keys. Empty graph → no-op.
func computeFileFanIn(s *store.Store) error {
	root, err := s.GetMeta("repo_root")
	if err != nil {
		return fmt.Errorf("read repo_root: %w", err)
	}
	raw, err := s.FileFanIn()
	if err != nil {
		return fmt.Errorf("compute file fan-in: %w", err)
	}
	if len(raw) == 0 {
		return nil
	}
	fanIn := make(map[string]float64, len(raw))
	for path, n := range raw {
		fanIn[RelToRoot(root, path)] = n
	}
	if err := s.WriteGraphMetrics("files", "fan_in", fanIn); err != nil {
		return fmt.Errorf("write file fan_in: %w", err)
	}
	return nil
}

// RelToRoot returns absPath relative to root, falling back to absPath when
// root is empty, absPath is already relative, or the paths share no base
// (mirrors store.toRelPath).
func RelToRoot(root, absPath string) string {
	if root == "" || !filepath.IsAbs(absPath) {
		return absPath
	}
	rel, err := filepath.Rel(root, absPath)
	if err != nil {
		return absPath
	}
	// Force forward slashes so these keys join against gitchurn.Walk paths
	// (git emits "/"-separated paths even on Windows, where Rel uses "\").
	return filepath.ToSlash(rel)
}

// computeChurn derives per-file git change-frequency (the temporal axis of
// the Tornhill hotspot model) and replaces the file_churn table. It always
// writes — even an empty result — so the table tracks HEAD: a repo that
// loses git history, or a non-git checkout, ends with no stale churn. Git
// absence is a clean no-op inside gitchurn.Walk, not an error.
func computeChurn(s *store.Store, log io.Writer) error {
	root, err := s.GetMeta("repo_root")
	if err != nil {
		return fmt.Errorf("read repo_root: %w", err)
	}
	if root == "" {
		return nil
	}
	t0 := time.Now()
	rows, err := gitchurn.Walk(root)
	if err != nil {
		return fmt.Errorf("walk git history: %w", err)
	}
	churn := make([]store.FileChurn, len(rows))
	for i, r := range rows {
		churn[i] = store.FileChurn{
			Path:        r.Path,
			Commits:     r.Commits,
			Authors:     r.Authors,
			FirstSeen:   r.FirstSeen,
			LastChanged: r.LastChanged,
			Score:       r.Score,

			BugCommits:     r.BugCommits,
			FeatureCommits: r.FeatureCommits,
			ChoreCommits:   r.ChoreCommits,
			OtherCommits:   r.OtherCommits,
			UntypedCommits: r.UntypedCommits,
		}
	}
	if err := s.WriteFileChurn(churn); err != nil {
		return fmt.Errorf("write file churn: %w", err)
	}
	fmt.Fprintf(log, "metrics: git churn computed for %d files in %dms\n",
		len(churn), time.Since(t0).Milliseconds())
	return nil
}
