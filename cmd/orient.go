package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/dkoosis/snipe/internal/context"
	"github.com/dkoosis/snipe/internal/output"
	"github.com/dkoosis/snipe/internal/query"
)

var (
	orientOut string
)

// orientCmd writes the full lintbrush-style orient bundle in one DB scan —
// context-full + deps-tree + every row-level metric + a content-addressed
// manifest (snipe-725). Replaces the 9-call bash loop in lintbrush's
// review.md prelude with one atomic snapshot.
var orientCmd = &cobra.Command{
	Use:     "orient",
	Short:   "Write a full orient bundle (context-full, deps-tree, all metrics, manifest) into a directory",
	GroupID: categoryOrient,
	Long: `Produces the standard lintbrush prelude bundle in one index open:

  context-full.json   — snipe context --full output
  deps-tree.json      — snipe deps --tree output
  metrics-<kind>.json — one file per row-level metric kind
  manifest.json       — { snipe_version, git_commit, index_fingerprint,
                          files: { path: sha256, ... }, generated_at }

The manifest is content-addressed so harnesses can use it as a CI cache key
at a given git SHA + index content.

Example:
  snipe orient --out .lintbrush/prelude
`,
	Args: cobra.NoArgs,
	RunE: runOrient,
}

func init() {
	orientCmd.Flags().StringVar(&orientOut, "out", "", "Output directory (created if missing)")
	rootCmd.AddCommand(orientCmd)
	knownSubcommands["orient"] = true
}

// orientMetricKinds is the row-level set that maps to one ReadTopN call each.
// Composite kinds (topo, cycles, coupling, distance, cyclo) are emitted via
// the same source data — we ship the rows; composition is the caller's call.
var orientMetricKinds = []string{
	"pagerank", "ca", "ce", "instability", "abstractness",
	"lcom4", "cyclo_max", "cyclo_p95", "cyclo_sum",
}

type orientManifest struct {
	SnipeVersion     string            `json:"snipe_version"`
	GitCommit        string            `json:"git_commit,omitempty"`
	IndexFingerprint string            `json:"index_fingerprint,omitempty"`
	GeneratedAt      string            `json:"generated_at"`
	Files            map[string]string `json:"files"` // path -> sha256 hex
	BundleSHA256     string            `json:"bundle_sha256"`
}

func runOrient(_ *cobra.Command, _ []string) error {
	start := time.Now()
	w := output.NewWriter(os.Stdout, false, GetOutputFormat())

	if orientOut == "" {
		return w.WriteError("orient", &output.Error{
			Code: output.ErrInternal, Message: "--out is required",
		})
	}
	if err := os.MkdirAll(orientOut, 0o755); err != nil {
		return w.WriteError("orient", &output.Error{
			Code: output.ErrInternal, Message: "mkdir out: " + err.Error(),
		})
	}

	s, dir, err := OpenStore(w, "orient")
	if err != nil {
		return err
	}
	defer s.Close()

	// 1. context --full
	ctx, err := context.Generate(context.GenerateConfig{
		RepoRoot: dir, DB: s.DB(), Full: true,
	})
	if err != nil {
		return w.WriteError("orient", &output.Error{
			Code: output.ErrInternal, Message: "context: " + err.Error(),
		})
	}
	if err := writeJSONFile(filepath.Join(orientOut, "context-full.json"), ctx); err != nil {
		return err
	}

	// 2. deps --tree (use the same FindDepGraph path runDepsTree uses)
	modulePath := query.DetectModulePath(s.DB())
	depsPayload := map[string]interface{}{}
	if modulePath != "" {
		if graph, err := query.FindDepGraph(s.DB(), modulePath); err == nil {
			edges := make([]output.DepTreeEdge, len(graph.Edges))
			for i, e := range graph.Edges {
				edges[i] = output.DepTreeEdge{From: e.From, To: e.To, FileCount: e.FileCount}
			}
			sort.Strings(graph.Packages)
			depsPayload["packages"] = graph.Packages
			depsPayload["edges"] = edges
			depsPayload["cycles"] = graph.Cycles
		}
	}
	if err := writeJSONFile(filepath.Join(orientOut, "deps-tree.json"), depsPayload); err != nil {
		return err
	}

	// 3. metrics-<kind>.json for each row-level kind
	for _, k := range orientMetricKinds {
		rows, err := s.ReadTopN("imports", k, 0)
		if err != nil {
			// skip kinds the index hasn't populated; downstream sees absence.
			continue
		}
		path := filepath.Join(orientOut, "metrics-"+k+".json")
		if err := writeJSONFile(path, rows); err != nil {
			return err
		}
	}

	// 4. content-addressed manifest
	files, err := hashOrientFiles(orientOut)
	if err != nil {
		return w.WriteError("orient", &output.Error{
			Code: output.ErrInternal, Message: "hash files: " + err.Error(),
		})
	}
	man := orientManifest{
		SnipeVersion: Version,
		GeneratedAt:  time.Now().UTC().Format(time.RFC3339),
		Files:        files,
		BundleSHA256: bundleHash(files),
	}
	if fp, err := s.GetMeta("fingerprint"); err == nil {
		man.IndexFingerprint = fp
	}
	if commit, err := s.GetMeta("git_commit"); err == nil {
		man.GitCommit = commit
	}
	if err := writeJSONFile(filepath.Join(orientOut, "manifest.json"), man); err != nil {
		return err
	}

	fmt.Printf("orient bundle · %d files · %s · %dms\n",
		len(files)+1, man.BundleSHA256[:12], time.Since(start).Milliseconds())
	return nil
}

func writeJSONFile(path string, v interface{}) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	return nil
}

func hashOrientFiles(dir string) (map[string]string, error) {
	out := map[string]string{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || e.Name() == "manifest.json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		out[e.Name()] = hex.EncodeToString(sum[:])
	}
	return out, nil
}

func bundleHash(files map[string]string) string {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		_, _ = h.Write([]byte(n))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(files[n]))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}
