# Convention Detection Design (#107)

## Goal

`snipe context --conventions` — detect coding conventions from the index via SQL queries. No file I/O, no AST parsing.

## Data Model

```go
type Conventions struct {
    Constructors  *ConstructorConvention  `json:"constructors,omitempty"`
    Receivers     *ReceiverConvention     `json:"receivers,omitempty"`
    Testing       *TestConvention         `json:"testing,omitempty"`
    Interfaces    *InterfaceConvention    `json:"interfaces,omitempty"`
    ErrorHandling *ErrorConvention        `json:"errors,omitempty"`
    FileOrg       *FileOrgConvention      `json:"file_organization,omitempty"`
```

Each category struct has:
- `Pattern string` — detected convention description
- `Confidence string` — "high" (>80%), "medium" (60-80%), "low" (<60%)
- Evidence fields (counts, ratios, examples)

## Detection Queries

1. **Constructors**: `symbols WHERE kind='func' AND name LIKE 'New%'` — parse signature for return style
2. **Receivers**: `symbols WHERE kind='method'` — classify receiver column: single-letter vs descriptive, pointer vs value
3. **Testing**: file_path patterns for colocated vs separate; func names for Test* helper patterns
4. **Interfaces**: `symbols WHERE kind='interface'` — `-er` suffix rate, method count distribution
5. **Errors**: `symbols WHERE kind='var' AND name LIKE 'Err%'` — sentinel error count
6. **File org**: type/struct/interface count per file — one-type-per-file vs multi-type

## Integration

- New file: `internal/context/conventions.go` (~250 lines)
- Types in: `internal/context/types.go`
- Flag: `--conventions` on context command
- Output: `output.Response[Conventions]` JSON envelope
- Boot: optional `Conventions` field on `BootContext`

## Testing

- Unit: in-memory SQLite with seeded symbols (same pattern as deps_test.go)
- Blackbox: index fixture repo, assert 4+ categories detected with confidence levels

## Acceptance

- Detects 6 convention categories
- JSON output with confidence levels
- Works on external repos (chi, cobra, bbolt)
- <50ms query time
