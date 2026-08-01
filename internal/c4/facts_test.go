package c4_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/dkoosis/snipe/internal/c4"
	"github.com/dkoosis/snipe/internal/index"
	"github.com/dkoosis/snipe/internal/store"
)

const (
	testModule    = "github.com/example/proj"
	testGoVersion = "1.26"
)

// newC4TestRepo opens a fresh store (full migrated schema) and writes a
// go.mod to a temp dir so goVersionFromMod has something to read. Returns the
// store and the repo dir to pass as BuildFacts' dir argument.
func newC4TestRepo(t *testing.T) (*store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	goMod := "module " + testModule + "\n\ngo " + testGoVersion + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.SetMeta("module_path", testModule); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMeta("repo_root", dir); err != nil {
		t.Fatal(err)
	}
	return s, dir
}

func addSymbol(t *testing.T, db *sql.DB, id, name, kind, pkgPath, filePath string, lineStart int) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO symbols (id, name, kind, pkg_path, file_path, line_start, col_start, line_end, col_end)
		VALUES (?, ?, ?, ?, ?, ?, 1, ?, 1)
	`, id, name, kind, pkgPath, filePath, lineStart, lineStart)
	if err != nil {
		t.Fatalf("insert symbol %s: %v", name, err)
	}
}

func addImport(t *testing.T, db *sql.DB, filePath, pkgPath, importerPkg string, line int) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO imports (file_path, pkg_path, line, col, importer_pkg)
		VALUES (?, ?, ?, 1, ?)
	`, filePath, pkgPath, line, importerPkg)
	if err != nil {
		t.Fatalf("insert import %s -> %s: %v", importerPkg, pkgPath, err)
	}
}

func TestBuildFacts_Container(t *testing.T) {
	s, dir := newC4TestRepo(t)
	addSymbol(t, s.DB(), "m1", "main", "func", testModule, filepath.Join(dir, "main.go"), 5)

	f, err := c4.BuildFacts(s, dir, c4.LevelContainer)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Containers) != 1 {
		t.Fatalf("want 1 container, got %d: %+v", len(f.Containers), f.Containers)
	}
	got := f.Containers[0]
	if got.Name != "proj" {
		t.Errorf("want container name %q, got %q", "proj", got.Name)
	}
	if got.Tech != "Go 1.26" {
		t.Errorf("want tech %q, got %q", "Go 1.26", got.Tech)
	}
	if got.Line != 5 {
		t.Errorf("want line 5, got %d", got.Line)
	}
}

func TestBuildFacts_Datastore(t *testing.T) {
	s, dir := newC4TestRepo(t)
	addImport(t, s.DB(), filepath.Join(dir, "internal/store/db.go"), "modernc.org/sqlite", testModule+"/internal/store", 3)

	f, err := c4.BuildFacts(s, dir, c4.LevelContainer)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Datastores) != 1 {
		t.Fatalf("want 1 datastore, got %d: %+v", len(f.Datastores), f.Datastores)
	}
	if f.Datastores[0].Name != "SQLite" {
		t.Errorf("want SQLite, got %q", f.Datastores[0].Name)
	}
	if len(f.Datastores[0].Evidence) != 1 || f.Datastores[0].Evidence[0] != testModule+"/internal/store" {
		t.Errorf("want evidence [%q], got %v", testModule+"/internal/store", f.Datastores[0].Evidence)
	}
}

// TestBuildFacts_Datastore_GroupsByTechnology is the sn-igsn regression: a
// technology imported from multiple packages (or via multiple driver
// packages that map to the same display name) must collapse into ONE
// Datastore row with every importer folded into Evidence, not one row per
// importer.
func TestBuildFacts_Datastore_GroupsByTechnology(t *testing.T) {
	s, dir := newC4TestRepo(t)
	addImport(t, s.DB(), filepath.Join(dir, "internal/store/db.go"), "modernc.org/sqlite", testModule+"/internal/store", 3)
	addImport(t, s.DB(), filepath.Join(dir, "cmd/root.go"), "modernc.org/sqlite", testModule+"/cmd", 7)
	addImport(t, s.DB(), filepath.Join(dir, "internal/query/q.go"), "database/sql", testModule+"/internal/query", 1)
	addImport(t, s.DB(), filepath.Join(dir, "internal/query/q_test.go"), "database/sql", testModule+"/internal/query_test", 2)

	f, err := c4.BuildFacts(s, dir, c4.LevelContainer)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Datastores) != 2 {
		t.Fatalf("want 2 datastore technologies (SQLite, SQL), got %d: %+v", len(f.Datastores), f.Datastores)
	}

	byName := make(map[string]c4.Datastore, len(f.Datastores))
	for _, d := range f.Datastores {
		byName[d.Name] = d
	}

	sqlite, ok := byName["SQLite"]
	if !ok {
		t.Fatalf("want a SQLite datastore, got %+v", f.Datastores)
	}
	wantSQLite := []string{testModule + "/cmd", testModule + "/internal/store"}
	if !reflect.DeepEqual(sqlite.Evidence, wantSQLite) {
		t.Errorf("want SQLite evidence %v (both importers, sorted), got %v", wantSQLite, sqlite.Evidence)
	}

	sqlRow, ok := byName["SQL"]
	if !ok {
		t.Fatalf("want a SQL datastore, got %+v", f.Datastores)
	}
	wantSQL := []string{testModule + "/internal/query", testModule + "/internal/query_test"}
	if !reflect.DeepEqual(sqlRow.Evidence, wantSQL) {
		t.Errorf("want SQL evidence %v (both importers, sorted), got %v", wantSQL, sqlRow.Evidence)
	}
}

// TestBuildFacts_Datastore_AnchoredMatchExcludesLookalike is a regression
// for the driverAllowlist's substring-match bug flagged on PR #214: a
// strings.Contains match on "badger" would misclassify any module merely
// containing that word as a path component (e.g. "badgerlint") as the
// Badger driver. matchesDriverPath anchors to full path segments instead.
func TestBuildFacts_Datastore_AnchoredMatchExcludesLookalike(t *testing.T) {
	s, dir := newC4TestRepo(t)
	addImport(t, s.DB(), filepath.Join(dir, "internal/lint/l.go"), "github.com/acme/badgerlint", testModule+"/internal/lint", 1)

	f, err := c4.BuildFacts(s, dir, c4.LevelContainer)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Datastores) != 0 {
		t.Errorf("want 0 datastores (badgerlint is not the Badger driver), got %+v", f.Datastores)
	}
}

// TestBuildFacts_Datastore_AnchoredMatchIncludesVersionedSubpackage checks
// the anchoring doesn't over-correct into false negatives: a real driver's
// versioned subpackage (e.g. pgx/v5) must still match.
func TestBuildFacts_Datastore_AnchoredMatchIncludesVersionedSubpackage(t *testing.T) {
	s, dir := newC4TestRepo(t)
	addImport(t, s.DB(), filepath.Join(dir, "internal/store/pg.go"), "github.com/jackc/pgx/v5", testModule+"/internal/store", 1)

	f, err := c4.BuildFacts(s, dir, c4.LevelContainer)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Datastores) != 1 || f.Datastores[0].Name != "PostgreSQL" {
		t.Errorf("want 1 PostgreSQL datastore (pgx/v5 versioned subpackage), got %+v", f.Datastores)
	}
}

func TestBuildFacts_ExternalEnv_GroupsByStrippedToken(t *testing.T) {
	s, dir := newC4TestRepo(t)
	refs := []index.StringRef{
		{ID: "1111111111111111", Value: "VOYAGE_API_URL", Kind: "env", FilePath: filepath.Join(dir, "a.go"), Line: 10},
		{ID: "2222222222222222", Value: "VOYAGE_MODEL", Kind: "env", FilePath: filepath.Join(dir, "a.go"), Line: 12},
		{ID: "3333333333333333", Value: "SNIPE_VOYAGE_API_KEY", Kind: "env", FilePath: filepath.Join(dir, "b.go"), Line: 3},
	}
	// Override the default test module to "snipe" so SNIPE_VOYAGE_API_KEY's
	// module-token strip is exercised the same way the design field's SNIPE
	// example exercises it on snipe itself (newC4TestRepo's default module,
	// "github.com/example/proj", wouldn't strip the SNIPE_ prefix at all).
	if err := s.SetMeta("module_path", "github.com/example/snipe"); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteLiterals(refs, ""); err != nil {
		t.Fatal(err)
	}

	f, err := c4.BuildFacts(s, dir, c4.LevelContext)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.External) != 1 {
		t.Fatalf("want 1 external system, got %d: %+v", len(f.External), f.External)
	}
	got := f.External[0]
	if got.Name != "VOYAGE" {
		t.Errorf("want name VOYAGE, got %q", got.Name)
	}
	if len(got.Evidence) != 3 {
		t.Errorf("want 3 evidence entries, got %d: %v", len(got.Evidence), got.Evidence)
	}
}

func TestBuildFacts_ExternalEnv_IgnoresNonMatchingSuffix(t *testing.T) {
	s, dir := newC4TestRepo(t)
	refs := []index.StringRef{
		{ID: "4444444444444444", Value: "SOME_RANDOM_VALUE", Kind: "env", FilePath: filepath.Join(dir, "a.go"), Line: 1},
	}
	if err := s.WriteLiterals(refs, ""); err != nil {
		t.Fatal(err)
	}

	f, err := c4.BuildFacts(s, dir, c4.LevelContext)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.External) != 0 {
		t.Errorf("want 0 external systems, got %d: %+v", len(f.External), f.External)
	}
}

func TestBuildFacts_ExternalImport_TestifyExcluded(t *testing.T) {
	s, dir := newC4TestRepo(t)
	addImport(t, s.DB(), filepath.Join(dir, "cmd/foo_test.go"), "github.com/stretchr/testify/assert", testModule+"/cmd", 4)
	addImport(t, s.DB(), filepath.Join(dir, "cmd/foo_test.go"), "github.com/google/go-cmp/cmp", testModule+"/cmd", 5)

	f, err := c4.BuildFacts(s, dir, c4.LevelContext)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range f.External {
		if e.Name == "github.com/stretchr/testify" || e.Name == "github.com/google/go-cmp" {
			t.Errorf("testify/go-cmp must never appear as an external system, got %+v", f.External)
		}
	}
}

func TestBuildFacts_ExternalImport_TestOnlyExcluded(t *testing.T) {
	s, dir := newC4TestRepo(t)
	// A non-allowlisted third-party import used only from a _test.go file must
	// be excluded (design field: "excluding test-only ... every importing file
	// is *_test.go").
	addImport(t, s.DB(), filepath.Join(dir, "cmd/foo_test.go"), "github.com/some/testonlylib", testModule+"/cmd", 4)

	f, err := c4.BuildFacts(s, dir, c4.LevelContext)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.External) != 0 {
		t.Errorf("want 0 external systems (test-only import excluded), got %+v", f.External)
	}
}

func TestBuildFacts_ExternalImport_ExcludesOwnModuleAndGolangX(t *testing.T) {
	s, dir := newC4TestRepo(t)
	addImport(t, s.DB(), filepath.Join(dir, "cmd/root.go"), testModule+"/internal/query", testModule+"/cmd", 1)
	addImport(t, s.DB(), filepath.Join(dir, "cmd/root.go"), "golang.org/x/tools/go/packages", testModule+"/cmd", 2)

	f, err := c4.BuildFacts(s, dir, c4.LevelContext)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.External) != 0 {
		t.Errorf("want 0 external systems, got %+v", f.External)
	}
}

func TestBuildFacts_MergesEnvAndImportForSameService(t *testing.T) {
	s, dir := newC4TestRepo(t)
	refs := []index.StringRef{
		{ID: "5555555555555555", Value: "STRIPE_API_KEY", Kind: "env", FilePath: filepath.Join(dir, "billing/pay.go"), Line: 20},
	}
	if err := s.WriteLiterals(refs, ""); err != nil {
		t.Fatal(err)
	}
	addImport(t, s.DB(), filepath.Join(dir, "billing/pay.go"), "github.com/stripe/stripe-go/v72", testModule+"/billing", 5)

	f, err := c4.BuildFacts(s, dir, c4.LevelContext)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.External) != 1 {
		t.Fatalf("want 1 merged external system, got %d: %+v", len(f.External), f.External)
	}
	got := f.External[0]
	if got.Kind != "env+import" {
		t.Errorf("want kind env+import, got %q", got.Kind)
	}
	if len(got.Evidence) != 2 {
		t.Errorf("want 2 evidence entries (env + import), got %d: %v", len(got.Evidence), got.Evidence)
	}
	// Regression: the import side's evidence must carry its real import
	// line (5), not a fabricated Line:1 that happens to look "earlier" than
	// the env hit's line (20) and silently wins the merge's evidence pick.
	if got.File != filepath.Join(dir, "billing/pay.go") || got.Line != 5 {
		t.Errorf("want import evidence pay.go:5, got %s:%d", got.File, got.Line)
	}
}

func TestBuildFacts_MergeDoesNotFalsePositiveOnPrefixOverlap(t *testing.T) {
	// Regression: mergeExternalSystems previously matched via
	// strings.Contains in both directions, so an env group whose name is a
	// substring of an unrelated import's org token (e.g. "OPEN" inside
	// "OPENAI") would incorrectly merge two distinct services into one.
	s, dir := newC4TestRepo(t)
	refs := []index.StringRef{
		{ID: "7777777777777777", Value: "OPEN_API_KEY", Kind: "env", FilePath: filepath.Join(dir, "a.go"), Line: 10},
	}
	if err := s.WriteLiterals(refs, ""); err != nil {
		t.Fatal(err)
	}
	addImport(t, s.DB(), filepath.Join(dir, "b.go"), "github.com/openai/openai-go", testModule+"/pkg", 3)

	f, err := c4.BuildFacts(s, dir, c4.LevelContext)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.External) != 2 {
		t.Fatalf("want 2 distinct external systems (OPEN env group, github.com/openai import), got %d: %+v", len(f.External), f.External)
	}
	for _, e := range f.External {
		if e.Kind == "env+import" {
			t.Errorf("want no merge between OPEN and github.com/openai, got merged system %+v", e)
		}
	}
}

func TestBuildFacts_ExternalImport_RealLineEvidence(t *testing.T) {
	// Regression: externalFromImports must report the import statement's
	// actual line, not a hardcoded Line:1 — a prior version's SQL never
	// selected `line`, so every import-detected external system reported
	// file:1 regardless of where the import really was. Uses an allowlisted
	// known-service import (no env correlation needed) so the case survives
	// the sn-04xb narrowing below.
	s, dir := newC4TestRepo(t)
	addImport(t, s.DB(), filepath.Join(dir, "cmd/root.go"), "github.com/twilio/twilio-go", testModule+"/cmd", 42)

	f, err := c4.BuildFacts(s, dir, c4.LevelContext)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.External) != 1 {
		t.Fatalf("want 1 external system, got %d: %+v", len(f.External), f.External)
	}
	got := f.External[0]
	if got.File != filepath.Join(dir, "cmd/root.go") || got.Line != 42 {
		t.Errorf("want root.go:42 (real import line), got %s:%d", got.File, got.Line)
	}
}

func TestBuildFacts_ExternalImport_OrdinaryLibraryExcluded(t *testing.T) {
	// sn-04xb: a bare third-party import with no env-var credential evidence
	// and no known-service allowlist match is a compile-time library
	// dependency (a CLI framework here), not a runtime external system — it
	// must not be reported as one.
	s, dir := newC4TestRepo(t)
	addImport(t, s.DB(), filepath.Join(dir, "cmd/root.go"), "github.com/alecthomas/kong", testModule+"/cmd", 3)

	f, err := c4.BuildFacts(s, dir, c4.LevelContext)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.External) != 0 {
		t.Errorf("want 0 external systems (ordinary library import), got %+v", f.External)
	}
}

func TestBuildFacts_MergedExternalSystem_FlowKeepsImporterPackage(t *testing.T) {
	s, dir := newC4TestRepo(t)
	refs := []index.StringRef{
		{ID: "6666666666666666", Value: "STRIPE_API_KEY", Kind: "env", FilePath: filepath.Join(dir, "billing/pay.go"), Line: 20},
	}
	if err := s.WriteLiterals(refs, ""); err != nil {
		t.Fatal(err)
	}
	addImport(t, s.DB(), filepath.Join(dir, "billing/pay.go"), "github.com/stripe/stripe-go/v72", testModule+"/billing", 5)

	f, err := c4.BuildFacts(s, dir, c4.LevelContainer)
	if err != nil {
		t.Fatal(err)
	}
	var flow *c4.Flow
	for i := range f.Flows {
		if f.Flows[i].To == "STRIPE" {
			flow = &f.Flows[i]
			break
		}
	}
	if flow == nil {
		t.Fatalf("want a flow to STRIPE, got %+v", f.Flows)
	}
	// A merged env+import system's flow must still attribute the real
	// importing package (testModule+"/billing"), not fall back to the
	// env-only "From: ''" branch just because the display name changed.
	if flow.From != testModule+"/billing" {
		t.Errorf("want From=%q, got %q (flow degraded to env-only despite import evidence)", testModule+"/billing", flow.From)
	}
}

func TestBuildFacts_FlowDeterminism(t *testing.T) {
	s, dir := newC4TestRepo(t)
	// Two occurrences of the same driver import from the same package, at
	// different files/lines — the flow must pick the lowest-sorted file, then
	// lowest line, not an arbitrary first match.
	addImport(t, s.DB(), filepath.Join(dir, "internal/store/z.go"), "modernc.org/sqlite", testModule+"/internal/store", 9)
	addImport(t, s.DB(), filepath.Join(dir, "internal/store/a.go"), "modernc.org/sqlite", testModule+"/internal/store", 3)

	f, err := c4.BuildFacts(s, dir, c4.LevelContainer)
	if err != nil {
		t.Fatal(err)
	}
	var flow *c4.Flow
	for i := range f.Flows {
		if f.Flows[i].Kind == "datastore" {
			flow = &f.Flows[i]
			break
		}
	}
	if flow == nil {
		t.Fatal("want a datastore flow, found none")
	}
	if flow.File != filepath.Join(dir, "internal/store/a.go") || flow.Line != 3 {
		t.Errorf("want a.go:3 (lowest file, then line), got %s:%d", flow.File, flow.Line)
	}
}

func TestBuildFacts_Component_RootPackageShortName(t *testing.T) {
	// Regression: componentShortPkg's fallback (pkg == module, e.g. the root
	// "main" package) must match cmd/diagram.go's shortPkg and return the
	// short base name, not the full module import path. A prior version
	// returned the full path for this one case, reachable on snipe's own
	// repo since main.go's importer_pkg is exactly the module path.
	s, dir := newC4TestRepo(t)
	addImport(t, s.DB(), filepath.Join(dir, "main.go"), testModule+"/cmd", testModule, 1)

	f, err := c4.BuildFacts(s, dir, c4.LevelComponent)
	if err != nil {
		t.Fatal(err)
	}
	var root *c4.Component
	for i := range f.Components {
		if f.Components[i].Name == "proj" {
			root = &f.Components[i]
			break
		}
	}
	if root == nil {
		names := make([]string, len(f.Components))
		for i, c := range f.Components {
			names[i] = c.Name
		}
		t.Fatalf("want a root component named %q (path.Base of module), got names %v", "proj", names)
	}
}

func TestBuildFacts_LevelContext_NoContainers(t *testing.T) {
	s, dir := newC4TestRepo(t)
	addSymbol(t, s.DB(), "m1", "main", "func", testModule, filepath.Join(dir, "main.go"), 5)

	f, err := c4.BuildFacts(s, dir, c4.LevelContext)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Containers) != 0 {
		t.Errorf("--level context must not include containers, got %+v", f.Containers)
	}
	if f.Module != testModule {
		t.Errorf("want module %q, got %q", testModule, f.Module)
	}
}
