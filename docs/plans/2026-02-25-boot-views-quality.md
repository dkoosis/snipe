# Boot Views Quality + Final Cleanup

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make `snipe context --boot` output useful for Claude orientation — filter noise from entry points, deepen flow traces, wire package summaries. Plus finish the last 2 cleanup items from #93.

**Architecture:** Boot views infrastructure exists (types, queries, wiring). The problems are quality: entry points list 18 init() functions, flows stop at cobra plumbing instead of reaching command logic, and package summaries aren't wired into boot output. Fix by filtering entry point queries, improving flow callee selection, and connecting existing `getPackagePurposes()` to `GenerateBoot()`.

**Tech Stack:** Go, SQLite, existing snipe index schema

---

### Task 1: Cleanup — Remove GetFileHash and unused config fields (delegate to sonnet)

**Files:**
- Modify: `internal/store/write.go:478-489` (remove GetFileHash)
- Modify: `internal/config/config.go:17-19` (remove Excludes, IndexPath fields)
- Modify: `internal/config/config.go:27-28` (remove from DefaultConfig)
- Modify: `internal/config/config.go:102-106` (remove from merge)

**Step 1: Remove GetFileHash from store/write.go**

Delete lines 478-489 (the `GetFileHash` method). No callers exist.

**Step 2: Remove Config.Excludes and Config.IndexPath**

In `internal/config/config.go`:
- Remove `Excludes []string` and `IndexPath string` from the Config struct
- Remove their initialization in `DefaultConfig()` (`Excludes: nil`, `IndexPath: ""`)
- Remove their merge logic in `merge()` (the `src.Excludes` and `src.IndexPath` blocks)

**Step 3: Run mage qa**

Run: `mage qa`
Expected: PASS (no callers for any of these)

**Step 4: Commit**

```bash
git add internal/store/write.go internal/config/config.go
git commit -m "refactor: remove dead GetFileHash and unused config fields (#93)"
```

---

### Task 2: Filter entry points — exclude init(), include RunE handlers

The current `GetEntryPointDetails()` and `queryEntryPointSymbols()` in `flows.go` match `name IN ('main', 'Execute', 'RunE', 'init')`. This surfaces 18 init() functions that just register cobra subcommands — noise for Claude.

**Files:**
- Modify: `internal/context/flows.go:68-101` (queryEntryPointSymbols)
- Modify: `internal/context/flows.go:278-305` (GetEntryPointDetails query)
- Test: `internal/context/flows_test.go` (new file)

**Step 1: Write test for entry point filtering**

Create `internal/context/flows_test.go`:

```go
package context

import (
    "testing"
)

func TestBuildFlowPath_SkipsShallowPaths(t *testing.T) {
    // Flow with only 1 node should return empty
    callGraph := map[string][]string{}
    symbolNames := map[string]string{"a": "main"}

    result := buildFlowPath("a", "main", "", callGraph, symbolNames, 5)
    if result != "" {
        t.Errorf("expected empty flow for leaf node, got %q", result)
    }
}

func TestBuildFlowPath_TracesCallChain(t *testing.T) {
    callGraph := map[string][]string{
        "a": {"b"},
        "b": {"c"},
    }
    symbolNames := map[string]string{
        "a": "main",
        "b": "Execute",
        "c": "Store.Open",
    }

    result := buildFlowPath("a", "main", "", callGraph, symbolNames, 5)
    want := "main -> Execute -> Store.Open"
    if result != want {
        t.Errorf("got %q, want %q", result, want)
    }
}
```

**Step 2: Run test to verify it passes (these test existing behavior)**

Run: `go test ./internal/context/ -run TestBuildFlowPath -v`
Expected: PASS

**Step 3: Modify queryEntryPointSymbols to exclude init()**

In `flows.go`, change `queryEntryPointSymbols` SQL WHERE clause. Replace:
```sql
AND (
    name = 'main'
    OR name = 'Execute'
    OR name = 'RunE'
    OR name = 'init'
)
```

With:
```sql
AND (
    name = 'main'
    OR name = 'Execute'
    OR name = 'RunE'
)
```

Remove `init` from the ORDER BY CASE too.

**Step 4: Apply same change to GetEntryPointDetails query**

In `GetEntryPointDetails()` (flows.go ~line 291), apply the same WHERE clause change — remove `OR s.name = 'init'` and the init CASE from ORDER BY.

Also remove the `LIMIT 20` — with init() gone, we'll have at most a handful of real entry points.

**Step 5: Run mage to verify**

Run: `mage`
Expected: PASS

**Step 6: Verify output improvement**

Run: `go run . context --boot 2>/dev/null | jq '.boot_views.entry_point_details | length'`
Expected: Small number (2-5 instead of 18+)

Run: `go run . context --boot 2>/dev/null | jq '.boot_views.entry_point_details[].name'`
Expected: "main", "Execute" — no init() functions

**Step 7: Commit**

```bash
git add internal/context/flows.go internal/context/flows_test.go
git commit -m "fix: filter init() from boot entry points — reduce noise for LLM orientation"
```

---

### Task 3: Improve flow depth — follow meaningful callees

The current `buildFlowPath()` follows the first unvisited callee at each level. This picks cobra plumbing (`isKnownSubcommandOrFlag`) over actual command logic. The fix: score callees by cross-package calls and ref count, preferring symbols that cross package boundaries.

**Files:**
- Modify: `internal/context/flows.go:162-216` (buildFlowPath)
- Test: `internal/context/flows_test.go` (add test)

**Step 1: Write test for improved callee selection**

Add to `flows_test.go`:

```go
func TestBuildFlowPath_PrefersExportedOverInternal(t *testing.T) {
    // When two callees exist, prefer the one with uppercase name (exported)
    callGraph := map[string][]string{
        "a": {"b", "c"},
    }
    symbolNames := map[string]string{
        "a": "Execute",
        "b": "isKnownSubcommandOrFlag",
        "c": "Store.Open",
    }

    result := buildFlowPath("a", "Execute", "", callGraph, symbolNames, 5)
    // Should prefer Store.Open over isKnownSubcommandOrFlag
    if result != "Execute -> Store.Open" {
        t.Errorf("got %q, want %q", result, "Execute -> Store.Open")
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/context/ -run TestBuildFlowPath_PrefersExported -v`
Expected: FAIL — current code picks first callee regardless of quality

**Step 3: Improve callee selection in buildFlowPath**

Replace the callee selection loop in `buildFlowPath()` (the inner for loop starting ~line 187). Instead of picking the first unvisited callee, score them:

```go
// Pick the best unvisited callee:
// 1. Prefer names with "." (cross-package method calls like Store.Open)
// 2. Prefer exported names (uppercase first letter)
// 3. Skip utility prefixes (is, has, get for simple getters)
var nextID string
var bestScore int
for _, calleeID := range callees {
    if visited[calleeID] {
        continue
    }
    name := symbolNames[calleeID]
    if name == "" {
        continue
    }
    // Skip fmt/log/strings utility calls
    if strings.HasPrefix(name, "fmt.") || strings.HasPrefix(name, "log.") || strings.HasPrefix(name, "strings.") {
        continue
    }

    score := 1
    // Prefer method calls (Type.Method pattern)
    if strings.Contains(name, ".") {
        score += 3
    }
    // Prefer exported (uppercase first char of name or after dot)
    parts := strings.SplitN(name, ".", 2)
    checkName := parts[len(parts)-1]
    if len(checkName) > 0 && checkName[0] >= 'A' && checkName[0] <= 'Z' {
        score += 2
    }
    // Deprioritize boolean helpers
    lower := strings.ToLower(checkName)
    if strings.HasPrefix(lower, "is") || strings.HasPrefix(lower, "has") {
        score -= 2
    }

    if score > bestScore {
        bestScore = score
        nextID = calleeID
    }
}
```

**Step 4: Run tests to verify**

Run: `go test ./internal/context/ -run TestBuildFlowPath -v`
Expected: All PASS

**Step 5: Verify output improvement**

Run: `go run . context --boot 2>/dev/null | jq '.boot_views.primary_flows'`
Expected: Deeper flows showing actual command logic, not just `main -> Execute -> isKnownSubcommandOrFlag`

**Step 6: Commit**

```bash
git add internal/context/flows.go internal/context/flows_test.go
git commit -m "fix: improve boot flow depth — prefer exported cross-package callees over helpers"
```

---

### Task 4: Wire package summaries into boot output

`GenerateBoot()` never populates the `Packages` field. `getPackagePurposes()` exists in architecture.go. Wire them together.

**Files:**
- Modify: `internal/context/generate.go:49-96` (GenerateBoot function)

**Step 1: Add package summaries to GenerateBoot**

In `GenerateBoot()`, after the `bootViews` line (~line 83), add:

```go
// Build package summaries
var packages []PackageRef
if purposes, err := getPackagePurposes(cfg.DB, cfg.RepoRoot); err == nil {
    for _, pp := range purposes {
        packages = append(packages, PackageRef{
            Name:    pp.Name,
            Purpose: pp.Purpose,
        })
    }
}
```

Then add `Packages: packages,` to the return struct.

**Step 2: Run mage to verify**

Run: `mage`
Expected: PASS

**Step 3: Verify output**

Run: `go run . context --boot 2>/dev/null | jq '.packages'`
Expected: Array of `{name, purpose}` objects for each package

**Step 4: Commit**

```bash
git add internal/context/generate.go
git commit -m "feat: wire package summaries into boot context output"
```

---

### Task 5: Verify and clean up

**Step 1: Run full QA**

Run: `mage qa`
Expected: PASS

**Step 2: Verify complete boot output**

Run: `go run . context --boot 2>/dev/null | jq '.'`

Check:
- `entry_point_details`: 2-5 entries, no init() functions
- `primary_flows`: Flows reach into actual command logic (3+ hops)
- `change_boundaries`: Present with persistence/cli/output/query groupings
- `packages`: Present with name + purpose for each package

**Step 3: Commit if any final adjustments needed**

---

## Execution Notes

- Task 1 is independent — delegate to sonnet in a worktree
- Tasks 2-4 are sequential (each builds on prior state)
- Task 5 is final verification
