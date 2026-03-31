# Context Output Quality Improvements

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Improve `snipe context --boot` output from B- to A-grade — cut from ~5k to ~3k tokens while increasing signal density.

**Architecture:** Nine targeted changes across `internal/context/`. The biggest win is compressing `entry_point_details` (60% of current tokens) into a command table with boilerplate pattern extraction. Remaining changes fix data quality issues in conventions, packages, flows, and build info. All changes are in the boot context generation path (`GenerateBoot` → `generateBootViews`).

**Tech Stack:** Go, SQLite (snipe index), `internal/context` package

**Verify command:** `make` (vet+lint+test)

**Generate & inspect output:** `go run . context --boot > contextllm.json`

---

## File Structure

| File | Responsibility | Change type |
|------|---------------|-------------|
| `internal/context/types.go` | Add `CommandTable`, `CommandEntry` types; update `BootViews` | Modify |
| `internal/context/flows.go` | Add `BuildCommandTable`; improve `pickBestCallee` terminal avoidance | Modify |
| `internal/context/flows_test.go` | Tests for command table compression and flow improvements | Modify |
| `internal/context/generate.go` | Wire command table into `generateBootViews`; fix `inferProjectPurpose` fallback | Modify |
| `internal/context/generate_test.go` | Test project purpose fallback | Modify |
| `internal/context/buildinfo.go` | Tighten `isValidMakeTarget` filter | Modify |
| `internal/context/buildinfo_test.go` | Test target filter rejects parsing artifacts | Modify |
| `internal/context/conventions.go` | Fix `detectFileOrg` pattern logic | Modify |
| `internal/context/conventions_test.go` | Test mixed file org pattern detection | Modify |
| `internal/context/architecture.go` | Fix vacuous package purposes; verify `_test` filtering | Modify |
| `internal/context/session.go` | Verify branch suppression on main/master | Verify only |
| `internal/context/ranking.go` | Ensure doc propagates to key_symbols | Verify only |

---

### Task 1: Fix build target parsing artifacts

**Issue #6 from feedback:** `"\\"` and `"/^[a-zA-Z0-9_-]+"` leak through `isValidMakeTarget` into `build_info.targets`.

**Files:**
- Modify: `internal/context/buildinfo.go:190-203` (`isValidMakeTarget`)
- Test: `internal/context/buildinfo_test.go`

- [ ] **Step 1: Write failing test for parsing artifacts**

Add to `buildinfo_test.go`:

```go
func TestIsValidMakeTarget_RejectsArtifacts(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"backslash", `\`, false},
		{"regex_fragment", `/^[a-zA-Z0-9_-]+`, false},
		{"empty", "", false},
		{"valid_simple", "test", true},
		{"valid_hyphen", "lint-fast", true},
		{"valid_underscore", "eval_setup", true},
		{"dollar_var", "$(FOO)", false},
		{"percent_pattern", "%.o", false},
		{"dot_target", ".DEFAULT", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isValidMakeTarget(tt.input); got != tt.want {
				t.Errorf("isValidMakeTarget(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/context/ -run TestIsValidMakeTarget_RejectsArtifacts -v`
Expected: FAIL — `\` and `.DEFAULT` cases fail.

- [ ] **Step 3: Fix `isValidMakeTarget`**

In `buildinfo.go`, replace the `isValidMakeTarget` function:

```go
// isValidMakeTarget filters out parsing artifacts from Makefile target extraction.
func isValidMakeTarget(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	// Reject names with shell/regex metacharacters or whitespace
	if strings.ContainsAny(name, " \t$()\\^[]{}|*+?%\"'") {
		return false
	}
	// Reject names starting with / (regex fragments) or . (special targets)
	if name[0] == '/' || name[0] == '.' {
		return false
	}
	return true
}
```

Key changes: added `%`, `"`, `'` to rejected chars; reject `.` prefix (`.PHONY`, `.DEFAULT` etc.); reject `/` prefix; added length cap.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/context/ -run TestIsValidMakeTarget -v`
Expected: PASS

- [ ] **Step 5: Run full check**

Run: `make`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/context/buildinfo.go internal/context/buildinfo_test.go
git commit -m "fix: reject Makefile parsing artifacts from build_info.targets"
```

---

### Task 2: Fix file_organization convention contradiction

**Issue #7 from feedback:** Claims "one type per file" with confidence "low" when data shows 28 single vs 26 multi (avg 3.3 types/file). Pattern label should be "mixed".

**Files:**
- Modify: `internal/context/conventions.go:325-379` (`detectFileOrg`)
- Test: `internal/context/conventions_test.go`

- [ ] **Step 1: Write failing test for near-equal split**

Add to `conventions_test.go`:

```go
func TestDetectFileOrg_NearEqual(t *testing.T) {
	db := setupConventionsDB(t)
	defer db.Close()

	// 3 single-type files
	insertSym(t, db, "f1", "Store", "struct", "/repo/store.go", "pkg", "", "")
	insertSym(t, db, "f2", "Index", "struct", "/repo/index.go", "pkg", "", "")
	insertSym(t, db, "f3", "Config", "struct", "/repo/config.go", "pkg", "", "")

	// 2 multi-type files (each with 3 types to push avg up)
	insertSym(t, db, "f4", "ReqA", "struct", "/repo/types_a.go", "pkg", "", "")
	insertSym(t, db, "f5", "ReqB", "struct", "/repo/types_a.go", "pkg", "", "")
	insertSym(t, db, "f6", "ReqC", "struct", "/repo/types_a.go", "pkg", "", "")
	insertSym(t, db, "f7", "ResA", "struct", "/repo/types_b.go", "pkg", "", "")
	insertSym(t, db, "f8", "ResB", "struct", "/repo/types_b.go", "pkg", "", "")
	insertSym(t, db, "f9", "ResC", "struct", "/repo/types_b.go", "pkg", "", "")

	result := detectFileOrg(db)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// 3 single vs 2 multi — 60% ratio, below 70% threshold
	if result.Pattern != "mixed (no dominant pattern)" {
		t.Errorf("pattern: got %q, want %q", result.Pattern, "mixed (no dominant pattern)")
	}
	if result.Confidence != "low" {
		t.Errorf("confidence: got %q, want low", result.Confidence)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/context/ -run TestDetectFileOrg_NearEqual -v`
Expected: FAIL — current code reports "one type per file" because 3 > 2 and ratio is 0.6 which hits the `default` case. Actually this may pass. Check and adjust test if needed.

- [ ] **Step 3: Verify existing behavior, adjust if needed**

The current threshold logic at `conventions.go:362-369`:
```go
switch {
case ratio >= 0.7 && singleType > multiType:
    pattern = "one type per file"
case ratio >= 0.7 && multiType > singleType:
    pattern = "multiple types per file"
default:
    pattern = "mixed (no dominant pattern)"
}
```

This is actually correct for the test case (ratio 0.6 < 0.7 → "mixed"). The real bug is that the production data (28 single, 26 multi) computes ratio as `max(28,26)/54 = 0.52` which correctly hits "mixed". But the output shows "one type per file". This means the production data may have changed since the review, OR the issue is that the pattern string is misleading when combined with avg_types_per_file of 3.3.

Check: if the test passes, the logic is fine but the confidence function may be producing "low" correctly. The real issue then is just that production data happened to hit the wrong bucket during the review. Verify by generating fresh output. If the test passes as-is, mark this task done — the code is correct, the reviewer saw stale output.

- [ ] **Step 4: Run full check**

Run: `make`
Expected: PASS

- [ ] **Step 5: Commit (if changes made)**

```bash
git add internal/context/conventions.go internal/context/conventions_test.go
git commit -m "test: add near-equal file org convention test"
```

---

### Task 3: Fix package purpose strings

**Issue #4 from feedback:** `internal/vector` → "Application logic", `bench` → "Application logic", `internal/util_test` → filename as purpose, `internal/index_test` duplicates real package purpose.

**Files:**
- Modify: `internal/context/architecture.go:118-180` (`inferPackagePurpose`)
- Test: `internal/context/architecture.go` (inline verification)

- [ ] **Step 1: Write failing test**

Create test in a new section of `generate_test.go` (architecture.go functions are tested there):

```go
func TestInferPackagePurpose_SpecificPackages(t *testing.T) {
	tests := []struct {
		pkg  string
		want string
	}{
		{"internal/vector", "Vector math for embedding similarity"},
		{"bench", "Benchmarks and baseline capture"},
		{"test/blackbox", "Integration tests (blackbox)"},
		{"test/bench", "Benchmarks and baseline capture"},
		{"internal/util", "Shared utility functions (project root, caching)"},
	}
	for _, tt := range tests {
		t.Run(tt.pkg, func(t *testing.T) {
			got := inferPackagePurpose(tt.pkg)
			if got != tt.want {
				t.Errorf("inferPackagePurpose(%q) = %q, want %q", tt.pkg, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/context/ -run TestInferPackagePurpose_SpecificPackages -v`
Expected: FAIL — `internal/vector` and `bench` return "Application logic".

- [ ] **Step 3: Verify `_test` package filtering works**

The `_test` filtering at `architecture.go:50-52` should already skip these. If _test packages still appear in output, the issue is that the output was generated before this code was committed. Verify by running `go run . context --boot | jq '.packages[] | select(.name | endswith("_test"))'`. If empty, filtering works. If not, check `normalizePackageName`.

- [ ] **Step 4: Update `inferPackagePurpose` map**

The map at `architecture.go:120-142` already has entries for `internal/vector` ("Vector math for embedding similarity") and `bench` ("Benchmarks and baseline capture"). Verify these are in the committed code. If they're only in the working tree, they need to be committed.

If the map entries are missing, add them:

```go
"internal/vector":  "Vector math for embedding similarity",
"bench":            "Benchmarks and baseline capture",
"test/bench":       "Benchmarks and baseline capture",
"test/blackbox":    "Integration tests (blackbox)",
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/context/ -run TestInferPackagePurpose -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/context/architecture.go internal/context/generate_test.go
git commit -m "fix: correct vacuous package purpose strings in context output"
```

---

### Task 4: Add project purpose fallback

**Issue #5 from feedback:** `"project": "snipe"` but no purpose field. The single most token-efficient orientation possible is missing.

**Files:**
- Modify: `internal/context/generate.go:839-854` (`inferProjectPurpose`)

- [ ] **Step 1: Write failing test**

Add to `generate_test.go`:

```go
func TestInferProjectPurpose_Fallback(t *testing.T) {
	// With no DB and no package docs, should try README or go.mod
	purpose := inferProjectPurpose(nil, "/nonexistent", "myproject")
	// nil DB returns empty — that's the current behavior
	if purpose != "" {
		t.Errorf("expected empty for nil DB, got %q", purpose)
	}
}

func TestInferProjectPurpose_FromReadme(t *testing.T) {
	// Create a temp dir with a README
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "README.md"), []byte(
		"# myproject\n\nA fast CLI tool for indexing Go code.\n\nMore stuff here.\n",
	), 0644)

	purpose := inferProjectPurpose(nil, dir, "myproject")
	if purpose == "" {
		t.Error("expected purpose from README, got empty")
	}
	if !strings.Contains(purpose, "indexing") && !strings.Contains(purpose, "CLI") {
		t.Errorf("purpose should mention key terms, got %q", purpose)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/context/ -run TestInferProjectPurpose -v`
Expected: FAIL — `FromReadme` test fails because current code only checks `package_docs` table.

- [ ] **Step 3: Add README fallback to `inferProjectPurpose`**

Replace `inferProjectPurpose` in `generate.go`:

```go
// inferProjectPurpose extracts a one-line project purpose.
// Checks: 1) package_docs table, 2) README first paragraph after heading.
func inferProjectPurpose(db *sql.DB, repoRoot string, projectName string) string {
	// Try package docs first (main module doc comment)
	if db != nil {
		var doc sql.NullString
		err := db.QueryRow(`
			SELECT doc FROM package_docs
			WHERE pkg_path LIKE '%/' || ? OR pkg_path = ?
			LIMIT 1
		`, projectName, projectName).Scan(&doc)
		if err == nil && doc.Valid && doc.String != "" {
			return ExtractFirstSentence(doc.String)
		}
	}

	// Fallback: extract first paragraph after heading from README
	for _, name := range []string{"README.md", "README", "readme.md"} {
		path := filepath.Join(repoRoot, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if purpose := extractReadmePurpose(string(data)); purpose != "" {
			return purpose
		}
	}

	return ""
}

// extractReadmePurpose extracts the first non-heading, non-empty paragraph from README content.
func extractReadmePurpose(content string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Skip headings, badges, empty lines
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "[!") || strings.HasPrefix(trimmed, "[![") {
			continue
		}
		return ExtractFirstSentence(trimmed)
	}
	return ""
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/context/ -run TestInferProjectPurpose -v`
Expected: PASS

- [ ] **Step 5: Run full check**

Run: `make`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/context/generate.go internal/context/generate_test.go
git commit -m "feat: add README fallback for project purpose in context output"
```

---

### Task 5: Ensure key_symbols include purpose strings

**Issue #3 from feedback:** `key_symbols` have role but no purpose/doc. Claude has to open the file to understand what `Result` or `SymbolRow` represents.

**Files:**
- Modify: `internal/context/generate.go:194-226` (`getKeySymbolsByRefCount`)
- Modify: `internal/context/ranking.go` (verify doc propagation)

- [ ] **Step 1: Verify ranked path already includes purpose**

Check `rankedToSymbolRefs` at `generate.go:148-163`. It already extracts `rs.Doc` and calls `ExtractFirstSentence`. Confirm `RankSymbols` at `ranking.go:71-87` includes doc in the query (`COALESCE(s.doc, '') as doc`). This path works — the issue is the fallback path.

- [ ] **Step 2: Write failing test for fallback path**

```go
func TestGetKeySymbolsByRefCount_IncludesPurpose(t *testing.T) {
	db := setupTestDB(t) // reuse existing test helper or create one
	defer db.Close()

	// Insert a symbol with a doc comment
	_, err := db.Exec(`INSERT INTO symbols (id, name, kind, file_path, line_start, doc)
		VALUES ('s1', 'Store', 'type', '/repo/store.go', 10, 'Store manages SQLite persistence.')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO refs (id, symbol_id, file_path, line, col)
		VALUES ('r1', 's1', '/repo/main.go', 5, 1)`)
	if err != nil {
		t.Fatal(err)
	}

	symbols := getKeySymbolsByRefCount(db, "/repo", 10)
	if len(symbols) == 0 {
		t.Fatal("expected at least one symbol")
	}
	if symbols[0].Purpose == "" {
		t.Error("expected purpose from doc comment, got empty")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/context/ -run TestGetKeySymbolsByRefCount_IncludesPurpose -v`
Expected: FAIL — current query doesn't select `doc`.

- [ ] **Step 4: Add doc to `getKeySymbolsByRefCount`**

In `generate.go`, update the function:

```go
func getKeySymbolsByRefCount(db *sql.DB, repoRoot string, limit int) []SymbolRef {
	var symbols []SymbolRef

	rows, err := db.Query(`
		SELECT s.name, s.file_path, s.line_start, COUNT(r.id) as ref_count,
			COALESCE(s.doc, '') as doc
		FROM symbols s
		LEFT JOIN refs r ON s.id = r.symbol_id
		WHERE s.file_path LIKE ? || '/%'
		  AND s.kind IN ('func', 'method', 'type', 'interface', 'struct')
		  AND s.name GLOB '[A-Z]*'
		GROUP BY s.id
		ORDER BY ref_count DESC
		LIMIT ?
	`, repoRoot, limit)
	if err != nil {
		return symbols
	}
	defer rows.Close()

	for rows.Next() {
		var ref SymbolRef
		var fullPath string
		var refCount int
		var doc string
		if err := rows.Scan(&ref.Name, &fullPath, &ref.Line, &refCount, &doc); err != nil {
			continue
		}
		ref.File = strings.TrimPrefix(fullPath, repoRoot+"/")
		if doc != "" {
			ref.Purpose = ExtractFirstSentence(doc)
		}
		symbols = append(symbols, ref)
	}

	return symbols
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/context/ -run TestGetKeySymbolsByRefCount -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/context/generate.go internal/context/generate_test.go
git commit -m "feat: include purpose strings in key_symbols from doc comments"
```

---

### Task 6: Verify active_work branch suppression

**Issue #9 from feedback:** `active_work.branch` shows "main" which doesn't orient. Should be suppressed on main/master.

**Files:**
- Verify: `internal/context/session.go:142-171` (`GetActiveWork`)

- [ ] **Step 1: Check working tree**

The git status shows `session.go` is modified. Check if the fix at line 167 is already there:

```go
// Only include branch when it's not main/master (feature branches orient better)
if s.Branch != "" && s.Branch != "main" && s.Branch != "master" {
    aw.Branch = s.Branch
}
```

- [ ] **Step 2: Verify with test**

Run: `go test ./internal/context/ -run TestGetActiveWork -v`

If there's an existing test covering branch suppression, confirm it passes. If not, add one:

```go
func TestGetActiveWork_SuppressesMainBranch(t *testing.T) {
	s := &Session{
		Branch: "main",
		Queries: []QueryRecord{
			{Symbol: "Foo", File: "foo.go", Line: 1, Command: "def"},
		},
	}
	aw := s.GetActiveWork()
	if aw == nil {
		t.Fatal("expected non-nil ActiveWork")
	}
	if aw.Branch != "" {
		t.Errorf("branch should be empty on main, got %q", aw.Branch)
	}
}
```

- [ ] **Step 3: Run and confirm**

Run: `go test ./internal/context/ -run TestGetActiveWork -v`
Expected: PASS (fix is already in working tree)

- [ ] **Step 4: Commit if new test added**

```bash
git add internal/context/session.go internal/context/session_test.go
git commit -m "test: verify active_work suppresses branch on main/master"
```

---

### Task 7: Compress entry_point_details into command table

**Issue #1 from feedback:** This is the biggest token win. 29 `run*` functions with repetitive callees consume ~60% of tokens. Most follow an identical boilerplate pattern (`GetOutputConfig → NewWriter → GetOutputFormat → OpenStore → Close`). Claude learns this from 2-3 examples; the other 26 are waste.

**Target:** Replace `entry_point_details []EntryPointRef` with `commands *CommandTable` — a compact command list with boilerplate pattern declared once and only notable deviations expanded.

**Files:**
- Modify: `internal/context/types.go` — add `CommandTable`, `CommandEntry`; update `BootViews`
- Modify: `internal/context/flows.go` — add `BuildCommandTable`
- Modify: `internal/context/generate.go` — wire into `generateBootViews`
- Test: `internal/context/flows_test.go`

- [ ] **Step 1: Add types**

In `types.go`, add after `EntryPointRef`:

```go
// CommandTable compresses entry point details into a compact command list.
// Declares the common boilerplate callee pattern once, then lists commands
// with only their purpose and any notable (non-boilerplate) callees.
type CommandTable struct {
	BoilerplatePattern []string       `json:"boilerplate_pattern" yaml:"boilerplate_pattern"`
	Commands           []CommandEntry `json:"commands" yaml:"commands"`
}

// CommandEntry describes a single CLI command entry point.
type CommandEntry struct {
	Name    string   `json:"name" yaml:"name"`
	Purpose string   `json:"purpose,omitempty" yaml:"purpose,omitempty"`
	Callees []string `json:"callees,omitempty" yaml:"callees,omitempty"` // only non-boilerplate callees
}
```

Update `BootViews` — replace `EntryPointDetails` with `Commands`:

```go
type BootViews struct {
	Commands         *CommandTable       `json:"commands,omitempty" yaml:"commands,omitempty"`
	PrimaryFlows     []string            `json:"primary_flows,omitempty" yaml:"primary_flows,omitempty"`
	ChangeBoundaries map[string][]string `json:"change_boundaries,omitempty" yaml:"change_boundaries,omitempty"`
	InterfaceMap     []InterfaceEntry    `json:"interface_map,omitempty" yaml:"interface_map,omitempty"`
}
```

Remove `EntryPointDetails` field (but keep `EntryPointRef` type — it's still used internally by `GetEntryPointDetails`).

- [ ] **Step 2: Write failing test for `BuildCommandTable`**

In `flows_test.go`:

```go
func TestBuildCommandTable_BoilerplateDetection(t *testing.T) {
	// Setup: create entry points where most share a common callee pattern
	details := []EntryPointRef{
		{Name: "runDef", Purpose: "Look up definitions", Callees: []string{"GetOutputConfig", "NewWriter", "OpenStore"}},
		{Name: "runRefs", Purpose: "Find references", Callees: []string{"GetOutputConfig", "NewWriter", "OpenStore"}},
		{Name: "runCallers", Callees: []string{"GetOutputConfig", "NewWriter", "OpenStore"}},
		{Name: "runContext", Purpose: "Generate boot context", Callees: []string{"FindProjectRoot", "GenerateBoot"}},
		{Name: "main", Callees: []string{"Execute"}},
	}

	table := compressToCommandTable(details)

	if table == nil {
		t.Fatal("expected non-nil command table")
	}

	// Boilerplate should contain the common callees
	boilerplateSet := make(map[string]bool)
	for _, c := range table.BoilerplatePattern {
		boilerplateSet[c] = true
	}
	if !boilerplateSet["GetOutputConfig"] || !boilerplateSet["NewWriter"] || !boilerplateSet["OpenStore"] {
		t.Errorf("boilerplate should contain common callees, got %v", table.BoilerplatePattern)
	}

	// main and Execute should be excluded (trivial entry points)
	for _, cmd := range table.Commands {
		if cmd.Name == "main" || cmd.Name == "Execute" {
			t.Errorf("trivial entry point %q should be excluded", cmd.Name)
		}
	}

	// runContext should have notable callees (non-boilerplate)
	var contextCmd *CommandEntry
	for i, cmd := range table.Commands {
		if cmd.Name == "runContext" {
			contextCmd = &table.Commands[i]
			break
		}
	}
	if contextCmd == nil {
		t.Fatal("expected runContext in commands")
	}
	if len(contextCmd.Callees) == 0 {
		t.Error("runContext should have notable callees (non-boilerplate)")
	}

	// runDef should have NO callees (all boilerplate)
	for _, cmd := range table.Commands {
		if cmd.Name == "runDef" && len(cmd.Callees) > 0 {
			t.Errorf("runDef callees should be empty (all boilerplate), got %v", cmd.Callees)
		}
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/context/ -run TestBuildCommandTable -v`
Expected: FAIL — `compressToCommandTable` doesn't exist yet.

- [ ] **Step 4: Implement `compressToCommandTable`**

In `flows.go`, add:

```go
// compressToCommandTable converts verbose entry point details into a compact command table.
// Detects the boilerplate callee pattern (callees appearing in >40% of commands),
// strips it from individual commands, and only includes notable deviations.
func compressToCommandTable(details []EntryPointRef) *CommandTable {
	if len(details) == 0 {
		return nil
	}

	// Filter out trivial entry points
	var commands []EntryPointRef
	for _, ep := range details {
		if ep.Name == "main" || ep.Name == "Execute" {
			continue
		}
		commands = append(commands, ep)
	}

	if len(commands) == 0 {
		return nil
	}

	// Count callee frequency across all commands (deduplicated per command)
	calleeFreq := make(map[string]int)
	for _, cmd := range commands {
		seen := make(map[string]bool)
		for _, c := range cmd.Callees {
			if !seen[c] {
				seen[c] = true
				calleeFreq[c]++
			}
		}
	}

	// Boilerplate = callees appearing in >40% of commands
	threshold := max(len(commands)*40/100, 2)
	boilerplate := make(map[string]bool)
	var boilerplateList []string
	for name, count := range calleeFreq {
		if count >= threshold {
			boilerplate[name] = true
			boilerplateList = append(boilerplateList, name)
		}
	}
	sort.Strings(boilerplateList)

	// Build compressed command entries
	var entries []CommandEntry
	for _, cmd := range commands {
		entry := CommandEntry{
			Name:    cmd.Name,
			Purpose: cmd.Purpose,
		}

		// Only include non-boilerplate callees
		seen := make(map[string]bool)
		var notable []string
		for _, c := range cmd.Callees {
			if !boilerplate[c] && !seen[c] {
				seen[c] = true
				notable = append(notable, c)
			}
		}
		if len(notable) > 0 {
			entry.Callees = notable
		}

		entries = append(entries, entry)
	}

	return &CommandTable{
		BoilerplatePattern: boilerplateList,
		Commands:           entries,
	}
}
```

Add `"sort"` to imports if not already present.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/context/ -run TestBuildCommandTable -v`
Expected: PASS

- [ ] **Step 6: Wire into `generateBootViews`**

In `generate.go`, update `generateBootViews`:

```go
func generateBootViews(db *sql.DB, repoRoot string) *BootViews {
	// Get entry point details using batch queries
	entryPointDetails, _ := GetEntryPointDetails(db, repoRoot)

	// Compress into command table
	var commands *CommandTable
	if len(entryPointDetails) > 0 {
		commands = compressToCommandTable(entryPointDetails)
	}

	// Get primary flows
	primaryFlows, _ := ExtractPrimaryFlows(db, repoRoot, 6)

	// Get change boundaries using batch query
	changeBoundaries, _ := GetChangeBoundaries(db, repoRoot)

	// Get interface satisfaction map
	interfaceMap, _ := GetInterfaceMap(db, repoRoot)

	if commands == nil && len(primaryFlows) == 0 && len(changeBoundaries) == 0 && len(interfaceMap) == 0 {
		return nil
	}

	return &BootViews{
		Commands:         commands,
		PrimaryFlows:     primaryFlows,
		ChangeBoundaries: changeBoundaries,
		InterfaceMap:     interfaceMap,
	}
}
```

- [ ] **Step 7: Update any code referencing `EntryPointDetails` field**

Search for `EntryPointDetails` across the codebase. If any code reads the old field, update it to use `Commands`. Common locations:
- Output formatting (if `BootViews` is serialized elsewhere)
- Tests that assert on `BootViews.EntryPointDetails`

- [ ] **Step 8: Run full check**

Run: `make`
Expected: PASS

- [ ] **Step 9: Generate output and verify compression**

Run: `go run . context --boot > /tmp/context-new.json && wc -l /tmp/context-new.json`

Verify:
- `commands.boilerplate_pattern` lists common callees
- Most commands have no `callees` field
- Commands with unique behavior (e.g., `runContext`, `runIncrementalIndex`) show only their notable callees
- Token count is significantly reduced from the ~60% that entry_point_details consumed

- [ ] **Step 10: Commit**

```bash
git add internal/context/types.go internal/context/flows.go internal/context/flows_test.go internal/context/generate.go
git commit -m "feat: compress entry_point_details into command table with boilerplate extraction"
```

---

### Task 8: Improve primary_flows depth and quality

**Issue #2 from feedback:** 10 flows, most dead-end at `(*Store).DB` — tells Claude the flow opens a database, not what it does. Flows stop too early to be useful.

**Target:** Fewer but deeper flows that reach meaningful terminals. Deprioritize infrastructure terminals (`DB`, `Close`), prefer callees that have their own callees (not dead-ends).

**Files:**
- Modify: `internal/context/flows.go:409-450` (`pickBestCallee`)
- Modify: `internal/context/flows.go:162-203` (`buildFlowPath`)
- Test: `internal/context/flows_test.go`

- [ ] **Step 1: Write failing test**

In `flows_test.go`:

```go
func TestPickBestCallee_AvoidsTerminals(t *testing.T) {
	// Setup: callee "DB" is a terminal (no further callees), "WriteIndex" has callees
	callGraph := map[string][]string{
		"writeIndex_id": {"doWork_id"},
	}
	symbolNames := map[string]string{
		"db_id":         "Store.DB",
		"close_id":      "Store.Close",
		"writeIndex_id": "Store.WriteIndex",
		"doWork_id":     "doWork",
	}
	visited := map[string]bool{}

	candidates := []string{"db_id", "close_id", "writeIndex_id"}
	best := pickBestCallee(candidates, visited, symbolNames, callGraph)

	if best != "writeIndex_id" {
		got := symbolNames[best]
		t.Errorf("expected WriteIndex (non-terminal), got %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/context/ -run TestPickBestCallee_AvoidsTerminals -v`
Expected: FAIL — current `pickBestCallee` doesn't take `callGraph` parameter and doesn't deprioritize terminals.

- [ ] **Step 3: Update `pickBestCallee` signature and logic**

In `flows.go`, update:

```go
// terminalMethods are infrastructure methods that don't reveal architectural intent.
var terminalMethods = map[string]bool{
	"DB": true, "Close": true, "String": true, "Error": true, "Err": true,
	"Bytes": true, "Len": true, "Reset": true,
}

// pickBestCallee selects the most architecturally significant callee from candidates.
// Prefers cross-package method calls and exported symbols. Avoids infrastructure
// terminals (DB, Close) and prefers callees that have their own callees (deeper paths).
func pickBestCallee(callees []string, visited map[string]bool, symbolNames map[string]string, callGraph map[string][]string) string {
	var bestID string
	var bestScore int

	for _, calleeID := range callees {
		if visited[calleeID] {
			continue
		}
		name := symbolNames[calleeID]
		if name == "" {
			continue
		}
		// Skip stdlib utility calls
		if strings.HasPrefix(name, "fmt.") || strings.HasPrefix(name, "log.") || strings.HasPrefix(name, "strings.") {
			continue
		}

		score := 1

		// Prefer method calls (Type.Method pattern)
		if strings.Contains(name, ".") {
			score += 3
		}

		// Prefer exported names
		parts := strings.SplitN(name, ".", 2)
		checkName := parts[len(parts)-1]
		if len(checkName) > 0 && checkName[0] >= 'A' && checkName[0] <= 'Z' {
			score += 2
		}

		// Deprioritize boolean helpers (isX, hasX)
		lower := strings.ToLower(checkName)
		if strings.HasPrefix(lower, "is") || strings.HasPrefix(lower, "has") {
			score -= 2
		}

		// Deprioritize infrastructure terminals
		if terminalMethods[checkName] {
			score -= 4
		}

		// Prefer callees that have their own callees (deeper paths available)
		if _, hasCallees := callGraph[calleeID]; hasCallees {
			score += 3
		}

		if score > bestScore {
			bestScore = score
			bestID = calleeID
		}
	}

	return bestID
}
```

- [ ] **Step 4: Update `buildFlowPath` to pass callGraph**

In `flows.go`, update the call at line ~185:

```go
nextID := pickBestCallee(callees, visited, symbolNames, callGraph)
```

The `callGraph` parameter is already available in `buildFlowPath`'s scope.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/context/ -run TestPickBestCallee -v`
Expected: PASS

- [ ] **Step 6: Update any existing `pickBestCallee` tests**

If there are existing tests for `pickBestCallee`, update them to pass the `callGraph` parameter. Search: `grep -n "pickBestCallee" internal/context/flows_test.go`

- [ ] **Step 7: Run full check**

Run: `make`
Expected: PASS

- [ ] **Step 8: Generate output and verify flow quality**

Run: `go run . context --boot | jq '.boot_views.primary_flows'`

Verify:
- Flows no longer dead-end at `.DB` or `.Close`
- Flows reach more meaningful terminals (e.g., `WriteIndex`, `ToJSONL`, `ExtractRefs`)
- 5 or fewer flows, each 3+ steps deep

- [ ] **Step 9: Commit**

```bash
git add internal/context/flows.go internal/context/flows_test.go
git commit -m "feat: improve primary_flows depth by avoiding infrastructure terminals"
```

---

### Task 9: Integration verification — measure output quality

**Files:**
- None modified — verification only

- [ ] **Step 1: Generate fresh output**

```bash
go run . context --boot > /tmp/context-after.json
```

- [ ] **Step 2: Measure token count**

```bash
wc -l /tmp/context-after.json
# Target: ~400-500 lines (~3k tokens), down from ~820 lines (~5k tokens)
```

- [ ] **Step 3: Spot-check each fix**

Verify with `jq`:

```bash
# Issue 1: Commands compressed
jq '.boot_views.commands.boilerplate_pattern' /tmp/context-after.json

# Issue 2: Flows don't dead-end at .DB
jq '.boot_views.primary_flows' /tmp/context-after.json

# Issue 3: Key symbols have purpose
jq '.key_symbols[] | select(.purpose != null and .purpose != "")' /tmp/context-after.json

# Issue 4: No "Application logic" for known packages
jq '.packages[] | select(.purpose == "Application logic")' /tmp/context-after.json

# Issue 5: Project has purpose
jq '.purpose' /tmp/context-after.json

# Issue 6: No parsing artifacts in targets
jq '.build_info.targets' /tmp/context-after.json

# Issue 7: File org pattern is "mixed" if data is near-equal
jq '.conventions.file_organization' /tmp/context-after.json

# Issue 9: No branch on main
jq '.active_work.branch' /tmp/context-after.json
# Should be null or absent
```

- [ ] **Step 4: Run full CI**

Run: `make ci`
Expected: PASS

- [ ] **Step 5: Commit output snapshot**

```bash
cp /tmp/context-after.json contextllm.json
git add contextllm.json
git commit -m "chore: update context output after quality improvements"
```

---

### Task 10: Session wrap

- [ ] **Step 1: Update `docs/progress.md`**

Record completed work and output quality metrics (before/after line count, token estimate).

- [ ] **Step 2: Rewrite `boot.md`**

New SHA, move context quality improvements to "done", update "do next".

- [ ] **Step 3: Commit**

```bash
git add docs/progress.md .claude/rules/boot.md
git commit -m "chore: wrap session — context output quality improvements"
```
