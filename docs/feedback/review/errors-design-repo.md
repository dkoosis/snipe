# errors-design review — snipe (repo scope)

Date: 2026-05-17 · Run: ecebe5258308 · Cap: 10

## Summary

Error surface is small and mostly clean. Only one project sentinel (`ErrSymbolNotFound`) — well-used. No `recover()` in project code. Wraps are generally informative (verb + noun + %w). Two real shape problems: callers string-matching on opaque messages, and a duplicated `WriteError`+`fmt.Errorf` pattern in `cmd/root.go` that puts the user-facing typed envelope and the program-flow error on parallel tracks.

Tier: P1 shape 🟡 · P1 wrap 🟢 · P1 handle-once 🟢 · P2 boundary n/a (CLI) · P2 typed-nil 🟢 · P3 recover 🟢

---

## Findings

### 1. shape-mismatch — caller string-matches "not found" to detect missing rg

- **Site:** `cmd/search.go:97` (consumer) · `internal/search/rg.go:48` (producer)
- **Issue:** `shape-mismatch`
- **Evidence:** Producer returns `fmt.Errorf("ripgrep (rg) not found: install from ...")`. Consumer branches on it via `strings.Contains(err.Error(), "not found")` to pick the `ErrRgNotFound` envelope code. Rename the producer's message and detection silently breaks.
- **Code:**
  ```go
  // internal/search/rg.go:48
  return nil, fmt.Errorf("ripgrep (rg) not found: install from https://github.com/BurntSushi/ripgrep")

  // cmd/search.go:95-99
  if err != nil {
      code := output.ErrInternal
      if strings.Contains(err.Error(), "not found") {
          code = output.ErrRgNotFound
      }
  ```
- **Fix:** Export `var ErrRgNotFound = errors.New("ripgrep not found")` in `internal/search`; return `fmt.Errorf("ripgrep not found: install from ...: %w", search.ErrRgNotFound)`; caller uses `errors.Is(err, search.ErrRgNotFound)`.

---

### 2. shape-mismatch — sqlite "no such table" detected by substring match

- **Site:** `internal/store/write.go:658-664` (`isNoSuchTableErr`)
- **Issue:** `shape-mismatch`
- **Evidence:** After `errors.As(err, &sqliteErr)` succeeds, the code falls back to `strings.Contains(sqliteErr.Error(), "no such table: " + table)`. modernc sqlite does expose result codes (e.g. `sqlite.ErrorCode`); the lowercase-substring dance is fragile to driver upgrades.
- **Code:**
  ```go
  func isNoSuchTableErr(err error, table string) bool {
      var sqliteErr *sqlite.Error
      if !errors.As(err, &sqliteErr) {
          return false
      }
      return strings.Contains(strings.ToLower(sqliteErr.Error()), "no such table: "+strings.ToLower(table))
  }
  ```
- **Fix:** Inspect `sqliteErr.Code()` against `sqlite3.SQLITE_ERROR` + check the specific error code if the driver exposes it; or pre-check the table exists via `sqlite_master` before the query. At minimum drop the `table` parameter — substring-matching just `"no such table"` is enough and removes a footgun where the table name embeds a regex meta-char.

---

### 3. shape-mismatch — `WriteError` + opaque `fmt.Errorf` flow control

- **Site:** `cmd/root.go:411-416`, `:421-425`, `:429-433`
- **Issue:** `shape-mismatch` (and partial `handle-twice`)
- **Evidence:** Three sibling paths write a typed `output.Error{Code: ...}` envelope to the writer **and then** return a different opaque `fmt.Errorf("index root mismatch")` / `"indexing in progress"` / `"index missing"`. Callers up the stack get a stringly-typed error and can't branch (e.g. exit code mapping). The typed code lives only on the JSON wire side; Go-side flow is duct tape.
- **Code:**
  ```go
  // cmd/root.go:421-426
  if store.IsIndexing(dbPath) {
      if w != nil {
          _ = w.WriteError(cmdName, output.NewIndexInProgressError())
      }
      return nil, root, fmt.Errorf("indexing in progress")
  }
  ```
- **Fix:** Declare sentinels alongside the `output.Err*` codes: `var ErrIndexMissing = errors.New("index missing")`, `ErrIndexInProgress`, `ErrIndexRootMismatch`. Return those directly. Callers and tests gain `errors.Is`; the envelope code keeps mapping cleanly.

---

### 4. wrap-redundant — `"failed to <X>: " + err.Error()` in envelope Message

- **Site:** `cmd/root.go:391`, `:441`
- **Issue:** `wrap-redundant` (style variant: concat instead of `%w`)
- **Evidence:** `Message: "failed to get working directory: " + err.Error()` and `Message: "failed to open index: " + err.Error()`. This is the `output.Error.Message` field, not a Go error chain — but the "failed to" prefix is the dying-metaphor noise that errors-design flags: the code is `ErrInternal`, the message just narrates. Either drop "failed to" (verb-noun is enough: "open index: %s") or pass `err.Error()` straight through.
- **Code:**
  ```go
  _ = w.WriteError(cmdName, &output.Error{
      Code:    output.ErrInternal,
      Message: "failed to open index: " + err.Error(),
  })
  ```
- **Fix:** `Message: fmt.Sprintf("open index: %s", err)` — matches the codebase's prevailing wrap style (verb-noun + colon) seen in `cmd/index.go`, `cmd/embed.go`, etc.

---

### 5. wrap-redundant — `"d2 render failed: %w"` after producer already says "d2"

- **Site:** `cmd/diagram.go:128`
- **Issue:** `wrap-redundant` (minor)
- **Evidence:** Surrounding context is already a `d2` rendering call; the prefix adds no information not present in the chain. Compare with line 121 (`"d2 CLI not found in PATH ..."`) which adds real install guidance — those wraps earn their place.
- **Code:**
  ```go
  if err := runD2(...); err != nil {
      return fmt.Errorf("d2 render failed: %w", err)
  }
  ```
- **Fix:** Drop the wrap (`return err`) or replace "d2 render failed" with the *input* context — e.g. `fmt.Errorf("render %s to svg: %w", inputPath, err)` — so the caller learns *what* failed to render, not that it was d2 (which the caller knows).

---

### 6. typed-error-fields-unread — `BatchError` looks like a Go error type but isn't

- **Site:** `internal/embed/batch.go:79-83`
- **Issue:** `typed-error-fields-unread` (terminology hazard)
- **Evidence:** `type BatchError struct { Code, Message string }` has no `Error()` method — it's a wire-format struct deserialized from Voyage AI's batch response, never raised as a Go error. The name reads like one. A reader skimming for typed errors (`errors.As` candidates) hits a false match.
- **Code:**
  ```go
  type BatchError struct {
      Code    string `json:"code"`
      Message string `json:"message"`
  }
  ```
- **Fix:** Rename to `BatchErrorPayload` or `BatchAPIError` to signal it's wire data, not a sentinel. Or give it an `Error() string` method and start raising it where batch responses carry one — the consumer at `batch.go:347-352` currently formats them into a fresh `fmt.Errorf` with no way to recover the upstream `Code`.

---

## Notes & non-findings

- **`ErrSymbolNotFound`** (`internal/edit/ast.go:20`) — well-shaped: callers test via `errors.Is` (`internal/edit/ast_test.go:97`), wrapped with `%w` + name + path at producer (`ast.go:162`). No action.
- **No `recover()` in project code** — only vendored deps. P3 clean.
- **No `log.Error(err); return err` anywhere in project** — the stderr `Warning:` writes in `cmd/embed.go`, `cmd/index.go`, `cmd/watch.go` all *swallow* the error (return nil / continue), which is the right call for best-effort paths. Not handle-twice.
- **`fmt.Errorf` count** — 457 sites (non-test, non-vendor); spot-checked across `cmd/`, `internal/store`, `internal/index`, `internal/embed`, `internal/query`, `internal/edit`. Wrap style is consistent (verb-noun + `: %w`) and no missing-arg defects found.
- **CLI = no transport boundary** — `boundary-leak-to-client` doesn't apply; raw error text reaches the user, which is correct for a developer tool.
- **No typed-nil-as-error patterns** detected — search for `return \w+Err$` after `var \w+Err *Type` came back empty.
