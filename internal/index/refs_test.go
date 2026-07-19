package index

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPosKey(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		line     int
		col      int
		expected string
	}{
		{
			name:     "simple case",
			file:     "/path/to/file.go",
			line:     10,
			col:      5,
			expected: "/path/to/file.go:10:5",
		},
		{
			name:     "multi-digit line and col",
			file:     "/path/to/file.go",
			line:     300,
			col:      42,
			expected: "/path/to/file.go:300:42",
		},
		{
			name:     "large line numbers",
			file:     "/path/to/file.go",
			line:     10000,
			col:      999,
			expected: "/path/to/file.go:10000:999",
		},
		{
			name:     "line 1 col 1",
			file:     "main.go",
			line:     1,
			col:      1,
			expected: "main.go:1:1",
		},
		{
			name:     "windows path with colon",
			file:     "C:/Users/test/file.go",
			line:     25,
			col:      10,
			expected: "C:/Users/test/file.go:25:10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := posKey(tt.file, tt.line, tt.col)
			if got != tt.expected {
				t.Errorf("posKey(%q, %d, %d) = %q, want %q",
					tt.file, tt.line, tt.col, got, tt.expected)
			}
		})
	}
}

func TestPosKeyDeterminism(t *testing.T) {
	// Same inputs should always produce the same output
	file := "/path/to/file.go"
	line := 300
	col := 42

	key1 := posKey(file, line, col)
	key2 := posKey(file, line, col)

	if key1 != key2 {
		t.Errorf("posKey is not deterministic: %q != %q", key1, key2)
	}
}

func TestPosKeyUniqueness(t *testing.T) {
	// Different inputs should produce different keys
	keys := make(map[string]bool)

	testCases := []struct {
		file string
		line int
		col  int
	}{
		{"/file.go", 1, 1},
		{"/file.go", 1, 2},
		{"/file.go", 2, 1},
		{"/other.go", 1, 1},
		{"/file.go", 11, 1}, // Should not collide with line 1, col 1
		{"/file.go", 1, 11}, // Should not collide with line 1, col 1
	}

	for _, tc := range testCases {
		key := posKey(tc.file, tc.line, tc.col)
		if keys[key] {
			t.Errorf("duplicate key generated: %s for (%s, %d, %d)",
				key, tc.file, tc.line, tc.col)
		}
		keys[key] = true
	}
}

func TestBuildSymbolPosIndex(t *testing.T) {
	symbols := []Symbol{
		{ID: "sym1", FilePath: "/file.go", NameLine: 10, NameCol: 5, Kind: KindFunc},
		{ID: "sym2", FilePath: "/file.go", NameLine: 20, NameCol: 10, Kind: KindFunc},
		{ID: "sym3", FilePath: "/other.go", NameLine: 10, NameCol: 5, Kind: KindFunc},
	}

	index := buildSymbolPosIndex(symbols)

	tests := []struct {
		file   string
		line   int
		col    int
		wantID string
		wantOK bool
	}{
		{"/file.go", 10, 5, "sym1", true},
		{"/file.go", 20, 10, "sym2", true},
		{"/other.go", 10, 5, "sym3", true},
		{"/file.go", 10, 6, "", false},    // wrong col
		{"/file.go", 11, 5, "", false},    // wrong line
		{"/missing.go", 10, 5, "", false}, // wrong file
		// Fallback tests for col 1 (chunked loading)
		{"/file.go", 10, 1, "sym1", true}, // fallback matches sym1
		{"/file.go", 20, 1, "sym2", true}, // fallback matches sym2
	}

	for _, tt := range tests {
		gotID, gotOK := index.Lookup(tt.file, tt.line, tt.col)
		if gotID != tt.wantID || gotOK != tt.wantOK {
			t.Errorf("Lookup(%s, %d, %d) = (%q, %v), want (%q, %v)",
				tt.file, tt.line, tt.col, gotID, gotOK, tt.wantID, tt.wantOK)
		}
	}
}

// TestExtractRefs_SignatureRefsGetEnclosingID verifies refs in a function
// signature (return type, params) are attributed to that function at index
// time — previously they were orphaned (enclosing range started at the body
// Lbrace) and recovered at the command layer.
func TestExtractRefs_SignatureRefsGetEnclosingID(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.com/sig\n\ngo 1.22\n",
		"sig.go": `package sig

type Widget struct{}

func MakeWidget(n int) *Widget {
	return &Widget{}
}
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	result, err := Load(LoadConfig{Dir: dir})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	symbols, err := ExtractSymbols(result)
	if err != nil {
		t.Fatalf("extract symbols: %v", err)
	}
	refs, err := ExtractRefs(result, symbols)
	if err != nil {
		t.Fatalf("extract refs: %v", err)
	}

	var makeWidgetID string
	for _, s := range symbols {
		if s.Name == "MakeWidget" {
			makeWidgetID = s.ID
		}
	}
	if makeWidgetID == "" {
		t.Fatal("MakeWidget symbol not found")
	}

	// The Widget ref on line 5 is in MakeWidget's signature (return type).
	var found bool
	for _, r := range refs {
		if r.Line == 5 {
			found = true
			if r.EnclosingID != makeWidgetID {
				t.Errorf("signature ref on line %d has enclosing_id %q, want MakeWidget %q",
					r.Line, r.EnclosingID, makeWidgetID)
			}
		}
	}
	if !found {
		t.Fatal("no ref found on MakeWidget's signature line")
	}
}

// TestExtractRefs_ASTCtx verifies the syntactic contexts recorded per ref.
func TestExtractRefs_ASTCtx(t *testing.T) {
	dir := t.TempDir()
	src := `package ctx

type Nug struct{ ID string }

type Box struct {
	N Nug
}

func MakeNug() *Nug {
	n := &Nug{ID: "x"}
	p := new(Nug)
	s := make([]Nug, 0)
	_ = p
	_ = s
	return n
}

func Save(n *Nug) {}

func use(n *Nug) {
	Save(n)
}
`
	files := map[string]string{
		"go.mod": "module example.com/ctx\n\ngo 1.22\n",
		"ctx.go": src,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	result, err := Load(LoadConfig{Dir: dir})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	symbols, err := ExtractSymbols(result)
	if err != nil {
		t.Fatalf("extract symbols: %v", err)
	}
	refs, err := ExtractRefs(result, symbols)
	if err != nil {
		t.Fatalf("extract refs: %v", err)
	}

	var nugID string
	for _, s := range symbols {
		if s.Name == "Nug" {
			nugID = s.ID
		}
	}

	wantByLine := map[int]string{
		6:  CtxTypeDecl,     // field type in Box
		9:  CtxSignature,    // return type of MakeNug
		10: CtxCompositeLit, // &Nug{ID: "x"}
		11: CtxNew,          // new(Nug)
		12: CtxMake,         // make([]Nug, 0)
	}
	got := map[int]string{}
	for _, r := range refs {
		if r.SymbolID == nugID {
			got[r.Line] = r.ASTCtx
		}
	}
	for line, want := range wantByLine {
		if got[line] != want {
			t.Errorf("line %d: ast_ctx = %q, want %q", line, got[line], want)
		}
	}
}

// TestExtractRefs_ConcurrencyCtx verifies synthetic self-attributed refs are
// emitted for go-statements and channel sends/receives, attributed to the
// enclosing named function. A go-statement/channel-op with no enclosing
// named func (e.g. inside a package-level IIFE) must NOT emit a ref.
func TestExtractRefs_ConcurrencyCtx(t *testing.T) {
	dir := t.TempDir()
	src := `package conc

func Spawn() {
	go func() { _ = 1 }()
}

func Chan() {
	ch := make(chan int, 1)
	ch <- 1
	<-ch
}

var _ = func() int {
	go func() { _ = 1 }()
	return 1
}()
`
	files := map[string]string{
		"go.mod":  "module example.com/conc\n\ngo 1.22\n",
		"conc.go": src,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	result, err := Load(LoadConfig{Dir: dir})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	symbols, err := ExtractSymbols(result)
	if err != nil {
		t.Fatalf("extract symbols: %v", err)
	}
	refs, err := ExtractRefs(result, symbols)
	if err != nil {
		t.Fatalf("extract refs: %v", err)
	}

	var spawnID, chanID string
	for _, s := range symbols {
		switch s.Name {
		case "Spawn":
			spawnID = s.ID
		case "Chan":
			chanID = s.ID
		}
	}
	if spawnID == "" || chanID == "" {
		t.Fatalf("missing symbol IDs: spawnID=%q chanID=%q", spawnID, chanID)
	}

	var goRefs, chanRefs []Ref
	for _, r := range refs {
		switch r.ASTCtx {
		case CtxGo:
			goRefs = append(goRefs, r)
		case CtxChan:
			chanRefs = append(chanRefs, r)
		}
	}

	// (a) exactly one go ref, self-attributed to Spawn. The package-level
	// IIFE's go-statement has no enclosing named func and must be skipped.
	if len(goRefs) != 1 {
		t.Fatalf("len(goRefs) = %d, want 1 (got %+v)", len(goRefs), goRefs)
	}
	if goRefs[0].EnclosingID != spawnID || goRefs[0].SymbolID != spawnID {
		t.Errorf("go ref = %+v, want EnclosingID=SymbolID=%q", goRefs[0], spawnID)
	}

	// (b) exactly two chan refs (send + receive), both self-attributed to Chan.
	if len(chanRefs) != 2 {
		t.Fatalf("len(chanRefs) = %d, want 2 (got %+v)", len(chanRefs), chanRefs)
	}
	for _, r := range chanRefs {
		if r.EnclosingID != chanID || r.SymbolID != chanID {
			t.Errorf("chan ref = %+v, want EnclosingID=SymbolID=%q", r, chanID)
		}
	}
}

// TestExtractRefs_RangeOverChannel verifies a `for v := range ch` receive is
// classified as a chan ref (sn-v84m), while ranging over a slice or map
// (no channel type) must NOT emit one.
func TestExtractRefs_RangeOverChannel(t *testing.T) {
	dir := t.TempDir()
	src := `package rng

func Drain(ch chan int) {
	for v := range ch {
		_ = v
	}
}

func Walk(xs []int) {
	for i := range xs {
		_ = i
	}
}

func Iter(m map[string]int) {
	for k := range m {
		_ = k
	}
}
`
	files := map[string]string{
		"go.mod": "module example.com/rng\n\ngo 1.22\n",
		"rng.go": src,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	result, err := Load(LoadConfig{Dir: dir})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	symbols, err := ExtractSymbols(result)
	if err != nil {
		t.Fatalf("extract symbols: %v", err)
	}
	refs, err := ExtractRefs(result, symbols)
	if err != nil {
		t.Fatalf("extract refs: %v", err)
	}

	var drainID, walkID, iterID string
	for _, s := range symbols {
		switch s.Name {
		case "Drain":
			drainID = s.ID
		case "Walk":
			walkID = s.ID
		case "Iter":
			iterID = s.ID
		}
	}
	if drainID == "" || walkID == "" || iterID == "" {
		t.Fatalf("missing symbol IDs: drainID=%q walkID=%q iterID=%q", drainID, walkID, iterID)
	}

	byEnclosing := map[string]int{}
	for _, r := range refs {
		if r.ASTCtx == CtxChan {
			byEnclosing[r.EnclosingID]++
		}
	}

	if byEnclosing[drainID] != 1 {
		t.Errorf("Drain: chan refs = %d, want 1 (for-range over chan must classify as concurrency)", byEnclosing[drainID])
	}
	if byEnclosing[walkID] != 0 {
		t.Errorf("Walk: chan refs = %d, want 0 (range over slice must not fire)", byEnclosing[walkID])
	}
	if byEnclosing[iterID] != 0 {
		t.Errorf("Iter: chan refs = %d, want 0 (range over map must not fire)", byEnclosing[iterID])
	}
}
