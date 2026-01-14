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

// TestCaptureOrcaBaseline captures metrics for the orca codebase
// Run with: go test -v -run TestCaptureOrcaBaseline ./test/bench/
func TestCaptureOrcaBaseline(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping orca baseline in short mode")
	}

	orcaDir := filepath.Join("..", "..", "..", "orca")
	if _, err := os.Stat(orcaDir); err != nil {
		t.Skip("orca not found at ../orca")
	}

	dir, _ := filepath.Abs(orcaDir)
	dbPath := filepath.Join(t.TempDir(), "orca.db")

	metrics := Metrics{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		GoVersion: runtime.Version(),
	}

	// Get git commit
	if commit, err := getGitCommitFor(dir); err == nil {
		metrics.GitCommit = commit
	}

	t.Logf("Indexing orca at %s...", dir)

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
	t.Logf("Load: %dms", loadEnd.Sub(loadStart).Milliseconds())

	// Extract
	extractStart := time.Now()
	syms, err := index.ExtractSymbols(result)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Symbols: %d", len(syms))

	refs, err := index.ExtractRefs(result, syms)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Refs: %d", len(refs))

	calls, err := index.ExtractCallGraph(result, syms)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Call edges: %d", len(calls))
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

	// Find a common symbol in orca
	testSymbol := "Server"

	// def by name
	start := time.Now()
	for i := 0; i < runs; i++ {
		query.LookupByName(s.DB(), testSymbol)
	}
	metrics.Query.DefByNameMs = float64(time.Since(start).Microseconds()) / float64(runs) / 1000.0

	// def by position (use a known file)
	pos := &query.PositionQuery{
		File: "internal/mcp/server/server.go",
		Line: 50,
		Col:  6,
	}
	start = time.Now()
	for i := 0; i < runs; i++ {
		query.ResolvePosition(s.DB(), pos)
	}
	metrics.Query.DefByPosMs = float64(time.Since(start).Microseconds()) / float64(runs) / 1000.0

	// refs by ID
	foundSyms, _ := query.LookupByName(s.DB(), testSymbol)
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
		search.Search(dir, "Handler.*Error", 50, 0)
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

	// Output JSON
	out, _ := json.MarshalIndent(metrics, "", "  ")
	t.Logf("Orca Baseline Metrics:\n%s", out)

	// Write to orca-specific baseline
	snipeDir, _ := filepath.Abs(filepath.Join("..", ".."))
	baselineFile := filepath.Join(snipeDir, "BASELINE_ORCA.json")
	if err := os.WriteFile(baselineFile, out, 0644); err != nil {
		t.Logf("Warning: could not write baseline file: %v", err)
	} else {
		t.Logf("Baseline written to: %s", baselineFile)
	}

	// Append to history
	historyFile := filepath.Join(snipeDir, ".snipe", "metrics_orca.jsonl")
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

func getGitCommitFor(dir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--short", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
