package cmd

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dkoosis/snipe/internal/store"
)

func TestTriageCmd_Registered(t *testing.T) {
	parser := newTestParser(t)
	for _, n := range parser.Model.Children {
		if n.Name == "triage" {
			return
		}
	}
	t.Error("triage command not registered")
}

// triageFixtureSymbol is one symbol row for the fixture index. Every symbol
// spans line 1 so query.FindSymbolAtPosition(rel, 1) — the exact path
// significance.sh's `snipe tests --at <file>:1:1` takes — resolves it, and
// has/without-tests is driven purely by whether a call_graph edge from a
// Test* function exists, not by symbol placement.
type triageFixtureSymbol struct {
	id, name, pkgPath, fileRel string
	lineStart, lineEnd         int
}

// buildTriageFixture creates a store with:
//   - internal/store/store.go: hotspot row present (cyclo/churn), has a test.
//   - cmd/other.go: hotspot row present (cyclo/churn), no test.
//   - cmd/new_thing.go: symbol indexed (so package resolves) but NO
//     cyclo_sum/churn entry — must surface as a null-score hotspot row.
func buildTriageFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	indexPath := store.DefaultIndexPath(root)
	s, err := store.Open(indexPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	if err := s.SetMeta("repo_root", root); err != nil {
		t.Fatalf("set repo_root: %v", err)
	}

	// sym-foo starts at line 15, NOT line 1 — this is what regression-proofs the
	// file-level test check: the old `--at <file>:1:1` idiom would miss it (no
	// symbol at line 1), reporting store.go as untested; hasFileTests must still
	// find it via FindSymbolsInFile over the whole file.
	symbols := []triageFixtureSymbol{
		{id: "sym-foo", name: "Foo", pkgPath: "example.com/store", fileRel: "internal/store/store.go", lineStart: 15, lineEnd: 20},
		{id: "sym-bar", name: "Bar", pkgPath: "example.com/cmd", fileRel: "cmd/other.go", lineStart: 3, lineEnd: 10},
		{id: "sym-baz", name: "Baz", pkgPath: "example.com/cmd", fileRel: "cmd/new_thing.go", lineStart: 2, lineEnd: 5},
		{id: "sym-testfoo", name: "TestFoo", pkgPath: "example.com/store", fileRel: "internal/store/store_test.go", lineStart: 4, lineEnd: 8},
	}
	for _, sym := range symbols {
		_, err := s.DB().Exec(`
			INSERT INTO symbols (id, name, kind, file_path, file_path_rel, pkg_path, line_start, col_start, line_end, col_end)
			VALUES (?, ?, 'func', ?, ?, ?, ?, 1, ?, 1)
		`, sym.id, sym.name, filepath.Join(root, sym.fileRel), sym.fileRel, sym.pkgPath, sym.lineStart, sym.lineEnd)
		if err != nil {
			t.Fatalf("insert symbol %s: %v", sym.id, err)
		}
	}

	// TestFoo -> Foo: gives internal/store/store.go a test; cmd/other.go and
	// cmd/new_thing.go get no call_graph edge, so they land in
	// files_without_tests.
	_, err = s.DB().Exec(`
		INSERT INTO call_graph (caller_id, callee_id, file_path, line, col)
		VALUES ('sym-testfoo', 'sym-foo', ?, 6, 1)
	`, filepath.Join(root, "internal/store/store_test.go"))
	if err != nil {
		t.Fatalf("insert call_graph: %v", err)
	}

	if err := s.WriteGraphMetrics("files", "cyclo_sum", map[string]float64{
		"internal/store/store.go": 120,
		"cmd/other.go":            30,
	}); err != nil {
		t.Fatalf("write cyclo_sum: %v", err)
	}
	if err := s.WriteGraphMetrics("files", "cyclo_max", map[string]float64{
		"internal/store/store.go": 40,
		"cmd/other.go":            10,
	}); err != nil {
		t.Fatalf("write cyclo_max: %v", err)
	}
	if err := s.WriteGraphMetrics("files", "cognitive_sum", map[string]float64{
		"internal/store/store.go": 80,
		"cmd/other.go":            15,
	}); err != nil {
		t.Fatalf("write cognitive_sum: %v", err)
	}
	if err := s.WriteGraphMetrics("files", "fan_in", map[string]float64{
		"internal/store/store.go": 49,
		"cmd/other.go":            5,
	}); err != nil {
		t.Fatalf("write fan_in: %v", err)
	}
	if err := s.WriteFileChurn([]store.FileChurn{
		{Path: "internal/store/store.go", Commits: 50, Authors: 3, LastChanged: "2026-06-01"},
		{Path: "cmd/other.go", Commits: 10, Authors: 1, LastChanged: "2026-05-01"},
	}); err != nil {
		t.Fatalf("write file churn: %v", err)
	}

	return root
}

// triageJSON decodes the bespoke `snipe triage --format=json` contract for
// assertions — deliberately its own struct (not triageResponse) so the test
// also pins the exact wire shape (score/fan_in/cyclo as JSON null, not 0).
type triageJSON struct {
	Files []struct {
		Path    string   `json:"path"`
		Score   *float64 `json:"score"`
		FanIn   *int     `json:"fan_in"`
		Cyclo   *int     `json:"cyclo"`
		Package string   `json:"package"`
	} `json:"files"`
	PackageCount      int      `json:"package_count"`
	FilesWithTests    []string `json:"files_with_tests"`
	FilesWithoutTests []string `json:"files_without_tests"`
}

func TestTriageCommand_BundlesHotspotsPackageAndTestsFacts(t *testing.T) {
	root := buildTriageFixture(t)
	t.Chdir(root)

	stdout, stderr, err := runCLI(t, "--format", "json", "triage",
		"internal/store/store.go", "cmd/other.go", "cmd/new_thing.go")
	if err != nil {
		t.Fatalf("triage returned error: %v (stderr=%q)", err, stderr)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("expected empty stderr, got %q", stderr)
	}

	var got triageJSON
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("unmarshal triage JSON: %v\nstdout=%s", err, stdout)
	}

	if len(got.Files) != 3 {
		t.Fatalf("got %d files, want 3: %+v", len(got.Files), got.Files)
	}

	byPath := map[string]int{}
	for i, f := range got.Files {
		byPath[f.Path] = i
	}

	// Indexed files with churn+cyclo data → real hotspot rows, not null.
	storeGo := got.Files[byPath["internal/store/store.go"]]
	if storeGo.Score == nil || storeGo.FanIn == nil || storeGo.Cyclo == nil {
		t.Errorf("internal/store/store.go should have non-null hotspot fields, got %+v", storeGo)
	}
	if storeGo.FanIn != nil && *storeGo.FanIn != 49 {
		t.Errorf("internal/store/store.go fan_in = %v, want 49", *storeGo.FanIn)
	}
	if storeGo.Cyclo != nil && *storeGo.Cyclo != 120 {
		t.Errorf("internal/store/store.go cyclo = %v, want 120", *storeGo.Cyclo)
	}
	if storeGo.Package != "example.com/store" {
		t.Errorf("internal/store/store.go package = %q, want example.com/store", storeGo.Package)
	}

	otherGo := got.Files[byPath["cmd/other.go"]]
	if otherGo.Score == nil || otherGo.FanIn == nil || otherGo.Cyclo == nil {
		t.Errorf("cmd/other.go should have non-null hotspot fields, got %+v", otherGo)
	}
	if otherGo.Package != "example.com/cmd" {
		t.Errorf("cmd/other.go package = %q, want example.com/cmd", otherGo.Package)
	}

	// null-not-omit: unindexed-for-hotspots file must still appear, with null
	// score/fan_in/cyclo, not be dropped the way raw `snipe hotspots` would.
	newThing := got.Files[byPath["cmd/new_thing.go"]]
	if newThing.Score != nil || newThing.FanIn != nil || newThing.Cyclo != nil {
		t.Errorf("cmd/new_thing.go hotspot fields should be null, got %+v", newThing)
	}
	if newThing.Package != "example.com/cmd" {
		t.Errorf("cmd/new_thing.go package = %q, want example.com/cmd (real package, not dirname)", newThing.Package)
	}

	// package_count = distinct REAL packages (example.com/store, example.com/cmd), not dirname count.
	if got.PackageCount != 2 {
		t.Errorf("package_count = %d, want 2", got.PackageCount)
	}

	// Every input file lands in exactly one of the two test-proximity buckets.
	if len(got.FilesWithTests) != 1 || got.FilesWithTests[0] != "internal/store/store.go" {
		t.Errorf("files_with_tests = %v, want [internal/store/store.go]", got.FilesWithTests)
	}
	wantWithout := map[string]bool{"cmd/other.go": true, "cmd/new_thing.go": true}
	if len(got.FilesWithoutTests) != 2 {
		t.Errorf("files_without_tests = %v, want 2 entries", got.FilesWithoutTests)
	}
	for _, f := range got.FilesWithoutTests {
		if !wantWithout[f] {
			t.Errorf("unexpected file in files_without_tests: %q", f)
		}
	}
}

// insertTriageSymbol adds one func symbol to the fixture store.
func insertTriageSymbol(t *testing.T, s *store.Store, root, id, name, fileRel string) {
	t.Helper()
	_, err := s.DB().Exec(`
		INSERT INTO symbols (id, name, kind, file_path, file_path_rel, pkg_path, line_start, col_start, line_end, col_end)
		VALUES (?, ?, 'func', ?, ?, 'example.com/p', 5, 1, 8, 1)
	`, id, name, filepath.Join(root, fileRel), fileRel)
	if err != nil {
		t.Fatalf("insert symbol %s: %v", id, err)
	}
}

// TestTriage_ExactPathMatch_NoSubstringCollision pins the P2 fix: a test in
// tools/cmd/other.go must not make cmd/other.go appear tested. The old
// FindSymbolsInFile `%pattern%` LIKE matched both files.
func TestTriage_ExactPathMatch_NoSubstringCollision(t *testing.T) {
	root := t.TempDir()
	s, err := store.Open(store.DefaultIndexPath(root))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	if err := s.SetMeta("repo_root", root); err != nil {
		t.Fatalf("set repo_root: %v", err)
	}

	// cmd/other.go: untested. tools/cmd/other.go: tested (substring superset).
	insertTriageSymbol(t, s, root, "sym-qux", "Qux", "cmd/other.go")
	insertTriageSymbol(t, s, root, "sym-bar", "Bar", "tools/cmd/other.go")
	insertTriageSymbol(t, s, root, "sym-testbar", "TestBar", "tools/cmd/other_test.go")
	if _, err := s.DB().Exec(`
		INSERT INTO call_graph (caller_id, callee_id, file_path, line, col)
		VALUES ('sym-testbar', 'sym-bar', ?, 6, 1)
	`, filepath.Join(root, "tools/cmd/other_test.go")); err != nil {
		t.Fatalf("insert call_graph: %v", err)
	}

	t.Chdir(root)
	stdout, _, err := runCLI(t, "--format", "json", "triage", "cmd/other.go")
	if err != nil {
		t.Fatalf("triage error: %v", err)
	}
	var got triageJSON
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, stdout)
	}
	if len(got.FilesWithTests) != 0 {
		t.Errorf("cmd/other.go must be untested (substring collision with tools/cmd/other.go), got with_tests=%v", got.FilesWithTests)
	}
}

// TestTriage_TestFileInput_CountsAsTested pins the P2 fix: a changed _test.go
// file that declares a test entry point counts as tested, rather than being
// reported untested because no other test calls it.
func TestTriage_TestFileInput_CountsAsTested(t *testing.T) {
	root := t.TempDir()
	s, err := store.Open(store.DefaultIndexPath(root))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	if err := s.SetMeta("repo_root", root); err != nil {
		t.Fatalf("set repo_root: %v", err)
	}

	insertTriageSymbol(t, s, root, "sym-testthing", "TestThing", "cmd/thing_test.go")

	t.Chdir(root)
	stdout, _, err := runCLI(t, "--format", "json", "triage", "cmd/thing_test.go")
	if err != nil {
		t.Fatalf("triage error: %v", err)
	}
	var got triageJSON
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, stdout)
	}
	if len(got.FilesWithTests) != 1 || got.FilesWithTests[0] != "cmd/thing_test.go" {
		t.Errorf("a _test.go file declaring TestThing must count as tested, got with_tests=%v without=%v", got.FilesWithTests, got.FilesWithoutTests)
	}
}

func TestTriageCommand_EmptyInput_ReturnsValidEmptyEnvelope(t *testing.T) {
	root := buildTriageFixture(t)
	t.Chdir(root)

	stdout, stderr, err := runCLI(t, "--format", "json", "triage")
	if err != nil {
		t.Fatalf("triage with no args returned error: %v (stderr=%q)", err, stderr)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("expected empty stderr, got %q", stderr)
	}

	// AXI #5: empty set-level groups serialize as [], never null.
	compact := strings.Join(strings.Fields(stdout), "")
	for _, key := range []string{`"files":[]`, `"files_with_tests":[]`, `"files_without_tests":[]`, `"package_count":0`} {
		if !strings.Contains(compact, key) {
			t.Errorf("stdout missing %s (want [] not null/omitted), got: %s", key, stdout)
		}
	}

	var got triageJSON
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("unmarshal triage JSON: %v\nstdout=%s", err, stdout)
	}
	if got.Files == nil || got.FilesWithTests == nil || got.FilesWithoutTests == nil {
		t.Errorf("decoded slices should be non-nil empty, got files=%v withTests=%v withoutTests=%v",
			got.Files, got.FilesWithTests, got.FilesWithoutTests)
	}
}

func TestTriageCommand_TextOutput_IsTerseAndNonCrashing(t *testing.T) {
	root := buildTriageFixture(t)
	t.Chdir(root)

	stdout, stderr, err := runCLI(t, "triage", "internal/store/store.go", "cmd/new_thing.go")
	if err != nil {
		t.Fatalf("triage text output returned error: %v (stderr=%q)", err, stderr)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("expected empty stderr, got %q", stderr)
	}
	if !strings.Contains(stdout, "triage ·") {
		t.Fatalf("stdout = %q, want a triage summary line", stdout)
	}
	if !strings.Contains(stdout, "internal/store/store.go") || !strings.Contains(stdout, "cmd/new_thing.go") {
		t.Fatalf("stdout = %q, want both file paths listed", stdout)
	}
}
