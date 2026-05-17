# /review json-shape — snipe (repo scope)

> Output: see plugins/dk/skills/house-style/SKILL.md

Tier: borderline

snipe is a CLI tool, not a service — JSON is the wire format between snipe and orca/Claude, not a public payment API. No float-money fields, no untrusted HTTP handlers, no custom MarshalJSON on first-party types. Most hazards reduce to two real problems: omitempty on numeric/bool fields where zero is meaningful (silent zero ⇒ absent collapse in golden tests + orca contract), and permissive decoders at the stdin/process trust boundary (`cmd/edit.go` batch reader, config loader). Score-style float64 fields (eval, metrics) are lossy-acceptable per the rules (ratios, p95, similarity scores) and not flagged.

## Scorecard

| Rule | Count |
|---|---|
| omitempty-loses-meaningful-zero | 4 |
| decoder-allows-unknown-fields | 3 |
| decode-target-any | 1 |
| time-format-drift | 1 |
| float-money | 0 (all are scores/ratios — lossy-acceptable) |
| marshal-no-roundtrip-test | 0 first-party |

Total: 9. All Tier: borderline — none are corruption-on-the-wire bugs, but several would catch real regressions cheaply.

## Findings

### 1. [F1] `internal/output/types.go:101` — omitempty-loses-meaningful-zero

**Diagnosis.** `Score float64 \`json:"score,omitempty"\`` drops a legitimately-zero match score from output.
**Why.** Search/rank results with `score=0` (no fuzzy match, exact-only hit) silently lose the field. Downstream orca/Claude can't distinguish "score not computed" from "score = 0". Golden tests over the Result struct will not catch a regression that flips a real score to zero.
**Evidence.** `Result.Score` in `internal/output/types.go:101`; emitted by `internal/output/json.go:1107` where `score` is computed and may legitimately land at 0.0 for exact-name matches.
**Fix.** Drop `omitempty`; or change to `*float64` (nil = not scored, 0 = explicit zero score).

Tier: borderline

### 2. [F2] `internal/output/types.go:122` — omitempty-loses-meaningful-zero

**Diagnosis.** `IsVariadic bool \`json:"is_variadic,omitempty"\`` on `FuncAnalysis`: `false` becomes absent.
**Why.** Variadic-ness is a binary property of the function under analysis. `is_variadic=false` is the answer for ~99% of funcs, not "we didn't check". omitempty makes "non-variadic" indistinguishable from "field not populated" — orca's consumer cannot tell them apart.
**Evidence.** `FuncAnalysis` at `internal/output/types.go:116-123`. Note `IsExported bool` (line 121) sibling is NOT omitempty — inconsistency confirms intent for `IsVariadic` is wrong.
**Fix.** Drop `,omitempty` to mirror `IsExported`.

Tier: action

### 3. [F3] `internal/output/types.go:29` — omitempty-loses-meaningful-zero

**Diagnosis.** `Priority int \`json:"priority,omitempty"\`` on Suggestion drops priority=0 from the wire.
**Why.** Comment on the field reads `// 1=high, 2=medium, 3=low` — so 0 is currently "unset". Fine today, but if any future suggestion is constructed without setting Priority, it silently emits with no priority and Claude infers "low priority" or guesses. Make absence vs. unset explicit.
**Evidence.** `internal/output/types.go:29`.
**Fix.** Either document `0=unset` in the comment + add a constructor that requires Priority, or change to `*int`.

Tier: borderline

### 4. [F4] `internal/output/types.go:47` — omitempty-loses-meaningful-zero

**Diagnosis.** `TokenEstimate int \`json:"token_estimate,omitempty"\`` on Meta — 0 tokens (e.g., empty result) drops the field.
**Why.** D4 ("token budget is first-class") makes this field load-bearing for orca telemetry. An empty response with `token_estimate=0` should serialize as `0`, not be absent — orca's `--caller`-side metric aggregation will skew if the field comes and goes.
**Evidence.** `internal/output/types.go:47`; consumed by orca integration per `.claude/rules/CLAUDE.md` "D4".
**Fix.** Drop omitempty. The field is a count; always emit it.

Tier: action

### 5. [F5] `cmd/edit.go:302` — decoder-allows-unknown-fields

**Diagnosis.** `json.NewDecoder(os.Stdin).Decode(&requests)` for `snipe edit` batch input has no `DisallowUnknownFields()`.
**Why.** This IS a trust boundary — stdin from orca, scripts, or hand-written batch files. A typo like `"newName"` instead of `"new_name"` decodes silently to the zero value; the edit then runs against the wrong target with empty new content, potentially corrupting source. The cost of a typo is a destructive code edit.
**Evidence.** `cmd/edit.go:299-307` runBatchEdit; `BatchEditRequest` is a known fixed schema.
**Fix.**
```go
dec := json.NewDecoder(os.Stdin)
dec.DisallowUnknownFields()
if err := dec.Decode(&requests); err != nil { ... }
```

Tier: action

### 6. [F6] `internal/config/config.go:82` — decoder-allows-unknown-fields

**Diagnosis.** Config file `json.Unmarshal(data, &cfg)` silently drops unknown keys.
**Why.** User edits `~/.snipe/config.json`, mistypes `embed_mode` as `embedding_mode`; snipe silently uses the default. The user spends time debugging "why isn't my config taking effect". DisallowUnknownFields would surface this as a startup error.
**Evidence.** `internal/config/config.go:72-87` loadFile; Config is a closed schema.
**Fix.** Switch to `json.NewDecoder(bytes.NewReader(data))` + `DisallowUnknownFields()` + `Decode(&cfg)`. Or run a separate strict validation pass; surface unknown-keys as a warning, not silent drop.

Tier: action

### 7. [F7] `internal/embed/batch.go:206,247,275` — decoder-allows-unknown-fields

**Diagnosis.** Three `json.NewDecoder(resp.Body).Decode(&result)` calls against Voyage AI batch responses, all without strict-mode.
**Why.** Borderline — Voyage adding fields shouldn't break us (forward-compat is desirable for an external API client). But this is the canonical "forward-compat by design" exception in the rule. Single report-only finding.
**Evidence.** `internal/embed/batch.go:206` (file upload), `:247` (batch create), `:275` (status poll).
**Fix.** Leave as-is — this is the documented exception. Add a one-line comment noting why strict-mode is intentionally not used, so future readers don't "fix" it.

Tier: borderline

### 8. [F8] `internal/search/rg.go:104,113` — decode-target-any

**Diagnosis.** `json.Unmarshal(line, &msg)` where the inner payload `msg.Data` is then decoded again as a typed shape — but `msg.Data` is held as `json.RawMessage` / interface and dispatched on `msg.Type`.
**Why.** ripgrep `--json` output has a documented union schema (begin/match/end/summary). Today snipe re-decodes per type at line 113 which is the correct pattern, but the outer typed shape isn't exhaustive — unknown `Type` values silently no-op. Low-risk in practice (rg's schema is stable); flagged once.
**Evidence.** `internal/search/rg.go:100-120`.
**Fix.** Add a default branch that logs `unexpected rg event type=%s` at debug; keeps the loop tolerant but observable.

Tier: borderline

### 9. [F9] `cmd/watch.go` + `internal/store/*` + `cmd/index.go` — time-format-drift

**Diagnosis.** All first-party timestamps use `time.RFC3339`, but they're inconsistently UTC-anchored.
**Why.** `cmd/watch.go:81,103,168,...` calls `time.Now().Format(time.RFC3339)` (local TZ), while `internal/store/write.go:708`, `internal/store/schema.go:363`, `internal/store/embed.go:15`, `internal/metrics/capture.go:89`, `internal/context/session.go:86,121`, `cmd/orient.go:148` all call `time.Now().UTC().Format(time.RFC3339)`. Same format, different zones — comparing a watch-event timestamp against a stored `indexed_at` will be wrong by up to TZ offset hours.
**Evidence.** Compare `cmd/watch.go:81` (no `.UTC()`) vs `internal/store/write.go:708` (`.UTC()`). Also `cmd/index.go:291,721,755` are local TZ; `cmd/orient.go:148` is UTC — even within `cmd/` the rule isn't consistent.
**Fix.** Pick one (UTC, since the majority is already UTC). Add a tiny helper `func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339) }` in `internal/timefmt` (or reuse an existing util pkg) and replace the ~15 call sites. One commit, one PR.

Tier: action

## Next

→ Address F2, F4, F5, F6, F9 (the `action` tier) — F5 (edit batch decoder) is the only one with a destructive-edit failure mode and should land first.
