# Claude Feedback: snipe Testing Session

**Date**: 2026-01-17
**Tested on**: orca codebase (645 files, 9855 symbols)
**Index time**: 4.3s
**Index size**: 33MB SQLite

## What Works Well

### Core Navigation (High Value)
| Command | Verdict | Notes |
|---------|---------|-------|
| `index` | Solid | Fast incremental, good stats output |
| `def <symbol>` | Solid | Ambiguity detection with candidates is helpful |
| `refs <symbol>` | Solid | Good context, enclosing function info |
| `callers <symbol>` | Solid | Call graph queries are fast |
| `search <text>` | Solid | Ripgrep integration works well |
| `show <id>` | Solid | ID lookup after finding symbol |
| `pkg <path>` | Solid | Great for understanding package API |
| `imports <file>` | Solid | Quick dependency check |
| `--with-body` | Solid | Essential for understanding implementations |

### Output Quality
- JSON-first is correct for LLM consumption
- `context.before/after` lines help me understand placement
- `edit_target` format is useful for potential edit operations
- `token_estimate` helps me budget context
- `match` showing full signature is helpful

## What's Broken

### P0: impl Command
```bash
snipe impl KnowledgeGraphWriter
# Returns: AMBIGUOUS_SYMBOL with candidates

snipe impl 0a304afcf1b96445  # Using candidate ID
# Returns: NOT_FOUND
```
**Impact**: Can't find interface implementations - critical for understanding Go codebases.

**Expected behavior**: Given an interface, return all types that implement it.

### P0: ID Disambiguation
```bash
snipe def SaveNugget
# Returns AMBIGUOUS_SYMBOL with 4 candidates, each with "id" field

snipe def ff35cb0e52f5f2c6  # Using the ID
# Returns: NOT_FOUND
```
**Impact**: When a symbol is ambiguous, I can't resolve it using the provided IDs.

**Expected behavior**: IDs returned in ambiguous errors should work as input to resolve the ambiguity.

### P1: importers Command
```bash
snipe importers internal/domain/state
# Returns: empty results (total: 0)

snipe importers github.com/dkoosis/orca/internal/domain/state
# Returns: empty results (total: 0)
```
**Impact**: Can't find what packages depend on a given package.

**Note**: The imports table has 3245 entries per index stats, so data exists.

### P2: callees Command
```bash
snipe callees NewWorkspace
# Returns: empty results (total: 0)
```
**Impact**: Can't trace what a function calls. Less critical since I can read the body, but useful for call graph exploration.

**Hypothesis**: Only tracks calls to symbols within the indexed codebase, not stdlib. If intentional, document this.

## Feature Requests

### High Priority

1. **File-qualified symbol syntax**: When ambiguous, let me specify:
   ```bash
   snipe def "internal/kg/mgmt/nuggets.go:SaveNugget"
   # or
   snipe def SaveNugget --file internal/kg/mgmt/nuggets.go
   ```

2. **Receiver-qualified method syntax**: For methods:
   ```bash
   snipe def "Service.SaveNugget"
   snipe callers "Workspace.EnsureWorkspace"
   ```

3. **Output filtering by kind**: When I get many results:
   ```bash
   snipe refs Workspace --kind=method  # Only method receivers
   snipe search handler --kind=func    # Only functions
   ```

### Medium Priority

4. **Struct field navigation**:
   ```bash
   snipe fields Workspace           # List struct fields
   snipe refs Workspace.Root        # Find usages of specific field
   ```

5. **Type hierarchy**:
   ```bash
   snipe embeds Workspace           # What types embed this?
   snipe embedded-by Service        # What does this type embed?
   ```

6. **Call chain depth**:
   ```bash
   snipe callers SaveNugget --depth=2  # Callers of callers
   ```

### Nice to Have

7. **Diff-aware results**: When I'm editing, show only symbols affected by recent changes
8. **Symbol rename preview**: `snipe rename OldName NewName --preview` showing all locations

## Output Format Suggestions

### Current (Good)
```json
{
  "id": "a77b78260d209b40",
  "file": "/Users/vcto/projects/orca/internal/domain/state/workspace.go",
  "range": { "start": {"line": 31, "col": 1}, "end": {"line": 41, "col": 2} },
  "kind": "func",
  "name": "NewWorkspace",
  "match": "func NewWorkspace(root string) (*Workspace, error)"
}
```

### Suggestion: Add relative paths
```json
{
  "file": "/Users/vcto/projects/orca/internal/domain/state/workspace.go",
  "rel_path": "internal/domain/state/workspace.go",  // <-- Add this
  ...
}
```
Absolute paths are verbose in output; relative paths are what I'll use in responses to users.

### Suggestion: Symbol ancestry for methods
```json
{
  "name": "SaveNugget",
  "receiver": "Service",           // <-- Add for methods
  "package": "internal/kg/mgmt",   // <-- Add package context
  ...
}
```

## Performance Notes

- Index build: 4.3s for 645 files - acceptable
- Query latency: 18-28ms - excellent
- The `index_state: "fresh"` indicator is helpful

## Summary for Future Claude Sessions

When using snipe on a Go codebase:

1. **Always run `snipe index` first** - subsequent queries are fast
2. **Use `def` for jumping to definitions** - handles ambiguity gracefully
3. **Use `refs` for find-all-references** - good context included
4. **Use `callers` for call graph** - works reliably
5. **Use `pkg` for package overview** - great for orientation
6. **Use `--with-body` when you need implementation details**
7. **Avoid `impl`** - currently broken
8. **Avoid `importers`** - returns empty
9. **When ambiguous**: Read the candidates, pick one, use `show <id>` to verify, then read the file directly at the line number

## Test Commands for Regression

```bash
# These should all work
snipe index
snipe def Workspace
snipe refs Workspace
snipe callers NewWorkspace
snipe show 325f0f0dc5b7ee18
snipe pkg internal/domain/state
snipe imports internal/domain/state/workspace.go
snipe search "TODO"
snipe def NewWorkspace --with-body

# These are currently broken (fix and verify)
snipe impl KnowledgeGraphWriter
snipe importers internal/domain/state
snipe callees NewWorkspace
snipe def ff35cb0e52f5f2c6  # ID from ambiguous result
```
