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
