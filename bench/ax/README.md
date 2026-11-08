# bench/ax — how a model routes a situation to a snipe command

The intent-routing bench for snipe's verb set (sn-lvub.2), feeding the verb-set decision sn-zvoz. Each call shows a model one help text and one situation; the model replies with the first command it would run. The report scores **verb right** and **whole command right** apart.

## Files

| File | What it is |
|---|---|
| `requests.jsonl` | One situation per line: the operation in meaning terms, and per variant the commands that count as right (`accept`), or as not wanted but not wrong (`defensible`). Requests sharing a `pair` letter differ in one feature. An accept names the arguments the command needs to run, and every accept and defensible rejects a `--help` reply: printing help is not doing the operation. |
| `variants/current.txt` | What `snipe --help` prints. `TestHelpVariantCurrent` in `cmd` fails when it drifts: `go run . --help > bench/ax/variants/current.txt`, then fix the `current` accepts. |
| `variants/proposed.txt` | The surface sn-zvoz recommends: `pack --depth sym\|pack\|explain`, `search --lits\|--trace\|--semantic`, `deps --importers\|--file`. |
| `results/<date>/` | `calls.jsonl` (one row per call) and `report.txt`. |

`TestAXBench_RequestsUseOnlyTheHelp` enforces the request rules: a situation names no verb from any variant, and every accept names a verb its variant offers. The runner's own check finds no verbs in kong's help layout, so this test is the check.

## Running it

The runner is mnemd's `cmd/axbench`, removed from mnemd in #336. Run it from a throwaway worktree at the last commit that had it:

```
git -C ~/projects/dk/mnemd worktree add --detach /tmp/mnemd-axbench 18f9eec
cd /tmp/mnemd-axbench
go run ./cmd/axbench run --dry-run --program snipe --bench <snipe>/bench/ax
go run ./cmd/axbench run --program snipe --bench <snipe>/bench/ax
go run ./cmd/axbench report --program snipe --bench <snipe>/bench/ax <snipe>/bench/ax/results/<date>
```

Each call is one isolated headless `claude -p` (no rules, tools or MCP). Three samples per cell: read a split cell as "rerun with more samples", not as a rate.

# bench/ax/output — how well a model reads snipe's output

The output-reading bench (sn-lvub.12), feeding the choice of snipe's default output shape. Each call shows a model one captured snipe result list in one shape and asks a question about the code with a checkable answer. The report scores **accuracy** and **mean input tokens** per shape per model.

## Files

| File | What it is |
|---|---|
| `output/cases.jsonl` | One question per line: the snipe command (`args`), the question, and the replies that count as right (`accept`; quoting, case and edge periods are ignored). |
| `output/captures/` | What each command printed in today's text (`--format concise --no-suggestions`), run on snipe's own repo. |
| `output/*.go` | The runner. It parses each capture and renders it in four shapes from that one parse, so every shape carries the same facts; `TestParseText_RoundTripsEveryCapture` proves the parse drops nothing. |
| `output/results/<date>/` | `calls.jsonl` (one row per call) and `report.txt`. |

The shapes: `text` (today's concise text), `oneline` (one line per item: `name [id] file:line kind sig`, then its extras), `toon` (one TOON tabular array) and `json` (compact JSON). No shape gets a preamble explaining it; the system prompt is the same for all.

## Running it

From the repo root, with `claude` logged in:

```
go run ./bench/ax/output run --dry-run bench/ax/output/results/x      # counts the calls
go run ./bench/ax/output run --parallel 6 bench/ax/output/results/$(date +%F)
go run ./bench/ax/output show callers-loc toon                         # what the model sees
```

The default is Sonnet and Opus, three samples per cell. Each call is one isolated headless `claude -p` in an empty directory (no settings, tools or MCP); a probe per model stops the run if it can see dk's rules.

**When the model generation changes**, rerun it as above with the new aliases (`--models sonnet,opus` resolves to the current ones; the report prints the exact model ids). Compare the new `report.txt` with the last one: a shape that beats `text` by more than noise on both models is a reason to revisit the default, and the decision nug for sn-lvub.12 holds the last numbers.

**When snipe's text output changes**, refresh the captures first and check each `accept` still matches:

```
go build -o /tmp/snipe . && /tmp/snipe index
go run ./bench/ax/output capture --snipe /tmp/snipe --repo .
```
