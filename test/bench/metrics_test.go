package bench

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dkoosis/snipe/internal/index"
	"github.com/dkoosis/snipe/internal/query"
	"github.com/dkoosis/snipe/internal/search"
	"github.com/dkoosis/snipe/internal/store"
)

// Metrics captures baseline measurements for snipe
type Metrics struct {
	Timestamp string         `json:"timestamp"`
	GitCommit string         `json:"git_commit,omitempty"`
	GoVersion string         `json:"go_version"`
	Codebase  CodebaseStats  `json:"codebase"`
	Index     IndexMetrics   `json:"index"`
	Query     QueryMetrics   `json:"query"`
	Search    SearchMetrics  `json:"search"`
	Quality   QualityMetrics `json:"quality"`
}

type CodebaseStats struct {
	GoFiles   int `json:"go_files"`
	Symbols   int `json:"symbols"`
	Refs      int `json:"refs"`
	CallEdges int `json:"call_edges"`
	DBSizeKB  int `json:"db_size_kb"`
}

type IndexMetrics struct {
	TotalMs   int64 `json:"total_ms"`
	LoadMs    int64 `json:"load_ms"`
	ExtractMs int64 `json:"extract_ms"`
	PersistMs int64 `json:"persist_ms"`
	PeakMemMB int   `json:"peak_mem_mb"`
}

type QueryMetrics struct {
	DefByNameMs float64 `json:"def_by_name_ms"`
	DefByPosMs  float64 `json:"def_by_pos_ms"`
	RefsByIDMs  float64 `json:"refs_by_id_ms"`
}

type SearchMetrics struct {
	SimplePatternMs float64 `json:"simple_pattern_ms"`
	RegexPatternMs  float64 `json:"regex_pattern_ms"`
}

type QualityMetrics struct {
	SymbolsWithDoc    int     `json:"symbols_with_doc"`
	SymbolsWithSig    int     `json:"symbols_with_sig"`
	DocCoverage       float64 `json:"doc_coverage_pct"`
	RefsPerSymbol     float64 `json:"refs_per_symbol"`
	CallGraphCoverage float64 `json:"callgraph_coverage_pct"`
}

// TestCaptureBaseline captures all metrics and outputs JSON
// Run with: go test -v -run TestCaptureBaseline ./test/bench/
func TestCaptureBaseline(t *testing.T) {
	if os.Getenv("SNIPE_BASELINE") == "" {
		t.Skip("skipping baseline capture (set SNIPE_BASELINE=1 to run)")
	}

	dir, _ := filepath.Abs(testRepo)
	dbPath := filepath.Join(t.TempDir(), "snipe.db")

	metrics := Metrics{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		GoVersion: runtime.Version(),
	}

	// --- Index metrics ---
	var memBefore, memAfter runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	indexStart := time.Now()

	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Load
	loadStart := time.Now()
	result, err := index.Load(index.LoadConfig{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	loadEnd := time.Now()

	// Extract
	extractStart := time.Now()
	syms, err := index.ExtractSymbols(result)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := index.ExtractRefs(result, syms)
	if err != nil {
		t.Fatal(err)
	}
	calls, err := index.ExtractCallGraph(result, syms)
	if err != nil {
		t.Fatal(err)
	}
	extractEnd := time.Now()

	// Persist
	persistStart := time.Now()
	if err := s.WriteIndex(syms, refs, calls); err != nil {
		t.Fatal(err)
	}
	persistEnd := time.Now()

	indexEnd := time.Now()
	runtime.ReadMemStats(&memAfter)

	metrics.Index.TotalMs = indexEnd.Sub(indexStart).Milliseconds()
	metrics.Index.LoadMs = loadEnd.Sub(loadStart).Milliseconds()
	metrics.Index.ExtractMs = extractEnd.Sub(extractStart).Milliseconds()
	metrics.Index.PersistMs = persistEnd.Sub(persistStart).Milliseconds()
	metrics.Index.PeakMemMB = int((memAfter.TotalAlloc - memBefore.TotalAlloc) / 1024 / 1024)

	// --- Codebase stats ---
	metrics.Codebase.Symbols = len(syms)
	metrics.Codebase.Refs = len(refs)
	metrics.Codebase.CallEdges = len(calls)

	// Count unique files
	files := make(map[string]bool)
	for _, sym := range syms {
		files[sym.FilePath] = true
	}
	metrics.Codebase.GoFiles = len(files)

	// DB size
	if fi, err := os.Stat(dbPath); err == nil {
		metrics.Codebase.DBSizeKB = int(fi.Size() / 1024)
	}

	// --- Query metrics (average of 100 runs) ---
	const runs = 100

	// def by name
	start := time.Now()
	for i := 0; i < runs; i++ {
		query.LookupByName(s.DB(), "Symbol")
	}
	metrics.Query.DefByNameMs = float64(time.Since(start).Microseconds()) / float64(runs) / 1000.0

	// def by position
	pos := &query.PositionQuery{
		File: "internal/store/store.go",
		Line: 20,
		Col:  6,
	}
	start = time.Now()
	for i := 0; i < runs; i++ {
		query.ResolvePosition(s.DB(), pos)
	}
	metrics.Query.DefByPosMs = float64(time.Since(start).Microseconds()) / float64(runs) / 1000.0

	// refs by ID
	foundSyms, _ := query.LookupByName(s.DB(), "Symbol")
	if len(foundSyms) > 0 {
		symbolID := foundSyms[0].ID
		start = time.Now()
		for i := 0; i < runs; i++ {
			query.FindRefs(s.DB(), symbolID, 100, 0)
		}
		metrics.Query.RefsByIDMs = float64(time.Since(start).Microseconds()) / float64(runs) / 1000.0
	}

	// --- Search metrics ---
	start = time.Now()
	for i := 0; i < runs; i++ {
		search.Search(dir, "func", 50, 0)
	}
	metrics.Search.SimplePatternMs = float64(time.Since(start).Microseconds()) / float64(runs) / 1000.0

	start = time.Now()
	for i := 0; i < runs; i++ {
		search.Search(dir, "func.*Error", 50, 0)
	}
	metrics.Search.RegexPatternMs = float64(time.Since(start).Microseconds()) / float64(runs) / 1000.0

	// --- Quality metrics ---
	withDoc := 0
	withSig := 0
	funcCount := 0
	for _, sym := range syms {
		if sym.Doc != "" {
			withDoc++
		}
		if sym.Signature != "" {
			withSig++
		}
		if sym.Kind == "function" || sym.Kind == "method" {
			funcCount++
		}
	}
	metrics.Quality.SymbolsWithDoc = withDoc
	metrics.Quality.SymbolsWithSig = withSig
	if len(syms) > 0 {
		metrics.Quality.DocCoverage = float64(withDoc) / float64(len(syms)) * 100
		metrics.Quality.RefsPerSymbol = float64(len(refs)) / float64(len(syms))
	}

	// Count funcs with outgoing call edges
	funcsWithCalls := make(map[string]bool)
	for _, c := range calls {
		funcsWithCalls[c.CallerID] = true
	}
	if funcCount > 0 {
		metrics.Quality.CallGraphCoverage = float64(len(funcsWithCalls)) / float64(funcCount) * 100
	}

	// Get git commit for tracking
	if commit, err := getGitCommit(); err == nil {
		metrics.GitCommit = commit
	}

	// Output JSON
	out, _ := json.MarshalIndent(metrics, "", "  ")
	t.Logf("Baseline Metrics:\n%s", out)

	// Write current baseline (pretty-printed)
	baselineFile := filepath.Join(dir, "BASELINE.json")
	if err := os.WriteFile(baselineFile, out, 0644); err != nil {
		t.Logf("Warning: could not write baseline file: %v", err)
	} else {
		t.Logf("Baseline written to: %s", baselineFile)
	}

	// Append to history (JSONL for trend tracking)
	historyFile := filepath.Join(dir, ".snipe", "metrics.jsonl")
	if err := os.MkdirAll(filepath.Dir(historyFile), 0755); err == nil {
		line, _ := json.Marshal(metrics)
		f, err := os.OpenFile(historyFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err == nil {
			f.Write(line)
			f.Write([]byte("\n"))
			f.Close()
			t.Logf("History appended to: %s", historyFile)
		}
	}
}

func getGitCommit() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--short", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
