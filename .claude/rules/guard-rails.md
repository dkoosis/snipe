---
description: Architectural guard rails
globs: ["**/*.go", "**/*.yaml"]
alwaysApply: true
---

# Guard Rails

## ‡ D1: Claude is the primary consumer

snipe exists to make it easier for Claude to work with Go repos. Every output format decision, every field included or excluded, every token spent — measured against: "does this help Claude understand the code?"

- Default output = what Claude reads directly. Terse, structured prose. No JSON envelope noise.
- `--format json` = orca/toolchain integration. Full envelope with protocol, meta, suggestions.
- If a field doesn't help Claude answer a question about the code, it doesn't belong in default output.

## ‡ D2: One command should "just work"

- Bare name → exact → case-insensitive → method-by-name → fuzzy → semantic
- User should never need to know receiver syntax, package path, or hex ID to start a query
- Ambiguity = show candidates with enough context to pick, not an error

## ‡ D4: Token budget is first-class

- Every byte of output must earn its place
- Envelope metadata (protocol version, decision_path, token_estimate) = toolchain concern, not Claude concern
- Suggestions are valuable only when actionable and non-obvious

## ✗ Do Not Build

Complex query language | multi-language support | LSP server | web UI | real-time file watching
