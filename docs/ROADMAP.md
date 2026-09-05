# snipe

★ Maps a Go codebase for LLMs orientation and navigation: symbols, callers, hotspots, metrics, in structured JSON.

(mirrors `docs/NORTH_STAR.md`, which owns the line — dk edits that file and nothing
else is a source.)

snipe is the navigator: it answers "where is this symbol, who calls it, what does it
touch" over a Go tree, in a shape an LLM can read. This file owns the epic inventory
below — what is done, what is underway, what comes next. Hand-written; agents do not
edit it unprompted.

## Epics

Ordered, one line per epic. Progress is never written here — it derives at read time
from the bd DAG joined against these ids.

1. [in progress] Instrument snipe queries → mine friction → improve resolver → sn-r1do
2. [deferred] Ship snipe via Homebrew → sn-b4b

## Non-goals

- Reviewing or auditing the code it maps. snipe locates; other tools judge.

## Resources

- Direction: `docs/NORTH_STAR.md`
- The queue: bd — `bd ready`, `bd show <id>`
- Conventions: `.claude/rules/`
