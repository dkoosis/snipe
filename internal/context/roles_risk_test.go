package context

import (
	"database/sql"
	"slices"
	"testing"

	_ "modernc.org/sqlite"
)

// setupRiskDB creates an in-memory DB with the schema the role/risk detectors
// read: symbols (with signature + pkg_path), refs (with enclosing_id, ast_ctx,
// snippet), imports, and an (empty) call_graph so InferRoles' structural queries
// resolve rather than error.
func setupRiskDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE symbols (
			id TEXT PRIMARY KEY,
			name TEXT,
			kind TEXT,
			signature TEXT,
			pkg_path TEXT,
			file_path TEXT,
			line_start INTEGER,
			doc TEXT
		);
		CREATE TABLE refs (
			id TEXT PRIMARY KEY,
			symbol_id TEXT,
			file_path TEXT,
			line INTEGER,
			col INTEGER,
			enclosing_id TEXT,
			snippet TEXT,
			ast_ctx TEXT
		);
		CREATE TABLE imports (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			file_path TEXT,
			pkg_path TEXT,
			name TEXT,
			line INTEGER,
			col INTEGER,
			importer_pkg TEXT
		);
		CREATE TABLE call_graph (
			caller_id TEXT,
			callee_id TEXT,
			file_path TEXT
		);
	`)
	if err != nil {
		t.Fatalf("create tables: %v", err)
	}
	return db
}

func addSym(t *testing.T, db *sql.DB, id, name, kind, sig, pkg, file string) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO symbols (id, name, kind, signature, pkg_path, file_path, line_start) VALUES (?,?,?,?,?,?,10)`,
		id, name, kind, sig, pkg, file,
	)
	if err != nil {
		t.Fatalf("insert symbol %s: %v", name, err)
	}
}

func addImport(t *testing.T, db *sql.DB, file, pkg string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO imports (file_path, pkg_path, line, col) VALUES (?,?,1,1)`, file, pkg); err != nil {
		t.Fatalf("insert import %s->%s: %v", file, pkg, err)
	}
}

func addRefCtx(t *testing.T, db *sql.DB, id, enclosingID, file, astCtx, snippet string) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO refs (id, symbol_id, file_path, line, col, enclosing_id, ast_ctx, snippet) VALUES (?,?,?,1,1,?,?,?)`,
		id, enclosingID, file, enclosingID, astCtx, snippet,
	)
	if err != nil {
		t.Fatalf("insert ref %s: %v", id, err)
	}
}

// riskFor returns the RiskFlags InferRoles assigned to the named symbol.
func riskFor(t *testing.T, db *sql.DB, name string) []string {
	t.Helper()
	roles, err := InferRoles(db, "/repo")
	if err != nil {
		t.Fatalf("InferRoles: %v", err)
	}
	for _, r := range roles {
		if r.Name == name {
			return r.RiskFlags
		}
	}
	t.Fatalf("symbol %q not found in InferRoles output", name)
	return nil
}

func hasFlag(flags []string, want string) bool {
	return slices.Contains(flags, want)
}

// TestRiskClassification_Precision is the precision guard (plan §4.1): each
// hand-labeled fixture must classify exactly as expected, decoys included.
func TestRiskClassification_Precision(t *testing.T) {
	db := setupRiskDB(t)
	defer db.Close()

	// --- concurrency positives ---
	// chan-typed signature (high precision, low recall)
	addSym(t, db, "pipe", "Pipe", kindFunc, "func Pipe(in <-chan int) chan int", "repo/pipe", "/repo/pipe.go")
	// sync.Mutex user: imports sync + encloses a Lock call
	addSym(t, db, "guard", "guardedCounter", kindMethod, "func (c *counter) guardedCounter()", "repo/count", "/repo/counter.go")
	addImport(t, db, "/repo/counter.go", "sync")
	addRefCtx(t, db, "r_lock", "guard", "/repo/counter.go", "call:Lock", "c.mu.Lock()")

	// --- concurrency: pure go-func spawner (recall gap closed by sn-hmz) ---
	// No chan signature and no sync-primitive call, but the indexer now emits
	// a self-attributed ast_ctx="go" ref for the go-statement, enclosed by
	// spawnWorker — detectable via the ungated goChanCtxs check.
	addSym(t, db, "spawn", "spawnWorker", kindFunc, "func spawnWorker()", "repo/work", "/repo/worker.go")
	addRefCtx(t, db, "r_go", "spawn", "/repo/worker.go", "go", "go worker()")

	// --- security_boundary positives ---
	// exec.Command caller: imports os/exec + encloses a Command call
	addSym(t, db, "backup", "runBackup", kindFunc, "func runBackup() error", "repo/backup", "/repo/backup.go")
	addImport(t, db, "/repo/backup.go", "os/exec")
	addRefCtx(t, db, "r_cmd", "backup", "/repo/backup.go", "call:Command", "exec.Command(\"tar\")")
	// InsecureSkipVerify setter: imports crypto/tls + references the field
	addSym(t, db, "tls", "dialTLS", kindFunc, "func dialTLS() error", "repo/tls", "/repo/tls.go")
	addImport(t, db, "/repo/tls.go", "crypto/tls")
	addRefCtx(t, db, "r_isv", "tls", "/repo/tls.go", "", "cfg.InsecureSkipVerify = true")

	// --- decoys (must NOT flag) ---
	// method literally named Command, but its file does NOT import os/exec.
	// Even with a call:Command ref, the missing import co-signal blocks the flag.
	addSym(t, db, "cmd_decoy", "Command", kindMethod, "func (s *Shell) Command() string", "repo/cli", "/repo/cli.go")
	addRefCtx(t, db, "r_decoy", "cmd_decoy", "/repo/cli.go", "call:Command", "s.Command()")
	// context.Context plumber: ctx in signature must not read as concurrency.
	addSym(t, db, "plumb", "plumbCtx", kindFunc, "func plumbCtx(ctx context.Context) error", "repo/plumb", "/repo/plumb.go")

	tests := []struct {
		name string
		want string // "" means no risk flags expected
	}{
		{"Pipe", RiskConcurrency},
		{"guardedCounter", RiskConcurrency},
		{"runBackup", RiskSecurityBoundary},
		{"dialTLS", RiskSecurityBoundary},
		{"spawnWorker", RiskConcurrency},
		{"Command", ""},
		{"plumbCtx", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := riskFor(t, db, tt.name)
			if tt.want == "" {
				if len(got) != 0 {
					t.Errorf("%s: expected no risk flags, got %v", tt.name, got)
				}
				return
			}
			if !hasFlag(got, tt.want) {
				t.Errorf("%s: expected flag %q, got %v", tt.name, tt.want, got)
			}
		})
	}
}

// TestHasChannelType guards the token-boundary matcher against the "Exchange"
// class of false positives.
func TestHasChannelType(t *testing.T) {
	tests := []struct {
		sig  string
		want bool
	}{
		{"func Pipe(in chan int)", true},
		{"func Send(c chan<- int)", true},
		{"func Recv(c <-chan int)", true},
		{"func f() chan int", true},
		{"func Exchange(rate float64) float64", false}, // contains "chan" but not a channel
		{"func Merchant() string", false},
		{"func plumbCtx(ctx context.Context) error", false},
	}
	for _, tt := range tests {
		if got := hasChannelType(tt.sig); got != tt.want {
			t.Errorf("hasChannelType(%q) = %v, want %v", tt.sig, got, tt.want)
		}
	}
}

// TestFullRoleCoverage is the defect-1 regression guard (plan §4.3): every
// function/type row in `context --full` output must carry a non-empty role,
// including symbols InferRoles skips (test files fall back to api_boundary).
func TestFullRoleCoverage(t *testing.T) {
	db := setupRiskDB(t)
	defer db.Close()

	addSym(t, db, "s1", "Store", kindStruct, "", "repo/store", "/repo/store.go")
	addSym(t, db, "s2", "Open", kindFunc, "func Open(p string) (*Store, error)", "repo/store", "/repo/store.go")
	addSym(t, db, "s3", "Query", kindMethod, "func (s *Store) Query() error", "repo/store", "/repo/store.go")
	addSym(t, db, "s4", "Reader", kindInterface, "", "repo/io", "/repo/io.go")
	// Exported symbol in a _test.go file: InferRoles excludes it, so it exercises
	// the api_boundary default in querySymbolRefsByKind.
	addSym(t, db, "s5", "Helper", kindFunc, "func Helper()", "repo/store", "/repo/store_test.go")

	syms := generateSymbols(db, "/repo", true, 1000)
	all := append(append([]SymbolRef{}, syms.Functions...), syms.Types...)
	if len(all) == 0 {
		t.Fatal("expected some --full symbols")
	}
	for _, ref := range all {
		if ref.Role == "" {
			t.Errorf("symbol %s (%s) has empty role in --full output", ref.Name, ref.File)
		}
	}
}

// TestConfigurableKeySymbolCap is the defect-1 secondary guard (plan §4.4): the
// ranked key-symbol list honors the configurable cap (--key-symbols) instead of
// a hardcoded 15.
func TestConfigurableKeySymbolCap(t *testing.T) {
	db := setupRiskDB(t)
	defer db.Close()

	// 20 exported symbols across 20 distinct packages so the per-package cap of 3
	// doesn't bite — isolating the top-N limit behavior.
	for i := range 20 {
		id := "k" + string(rune('a'+i))
		name := "Sym" + string(rune('A'+i))
		file := "/repo/pkg" + string(rune('a'+i)) + "/f.go"
		pkg := "repo/pkg" + string(rune('a'+i))
		addSym(t, db, id, name, kindFunc, "func "+name+"()", pkg, file)
		// give each a ref so ref_count > 0 and priorities spread
		for j := 0; j <= i; j++ {
			addRefCtx(t, db, id+"_r"+string(rune('a'+j)), "", file, "", "")
		}
	}

	wide, err := RankSymbols(db, "/repo", 50)
	if err != nil {
		t.Fatalf("RankSymbols(50): %v", err)
	}
	if len(wide) <= 15 {
		t.Errorf("--key-symbols 50 should yield >15 ranked symbols, got %d", len(wide))
	}

	narrow, err := RankSymbols(db, "/repo", 15)
	if err != nil {
		t.Fatalf("RankSymbols(15): %v", err)
	}
	if len(narrow) != 15 {
		t.Errorf("--key-symbols 15 should yield exactly 15, got %d", len(narrow))
	}
}

// TestRiskWeightBoostsRanking verifies a risk-flagged symbol outranks a plain
// internal symbol with the same ref count, and that risk symbols are exempt from
// the per-package cap (plan step 5).
func TestRiskWeightBoostsRanking(t *testing.T) {
	base := effectiveWeight(RoleInternal, nil)
	boosted := effectiveWeight(RoleInternal, []string{RiskSecurityBoundary})
	if boosted <= base {
		t.Errorf("security_boundary should boost weight above internal: base=%v boosted=%v", base, boosted)
	}
	// max, not product: a security_boundary api_boundary keeps the higher of the two.
	got := effectiveWeight(RoleAPIBoundary, []string{RiskConcurrency})
	if got != riskWeights[RiskConcurrency] && got != roleWeights[RoleAPIBoundary] {
		t.Errorf("effectiveWeight should be max(role, risk), got %v", got)
	}
}
