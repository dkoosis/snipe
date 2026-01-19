# Scenario: Understand Flow

Trace data flow through a call chain using snipe.

## Setup

Provide Claude with:
- An entry point function name
- The package or file it lives in
- A question about how data flows

Example prompt:
> "Starting from `HandleRequest` in `internal/server/handler.go`, trace how the request body gets validated and processed. Explain the flow."

## Goal

Navigate through 3+ functions to explain how data transforms or flows through the system.

## Expected Commands

| Command | Purpose |
|---------|---------|
| `def` | Jump into called functions |
| `callers` | Understand who invokes a function |
| body reading | Understand what each function does |

## Success Criteria

- [ ] Traced through at least 3 functions
- [ ] Correctly explained what each function does
- [ ] Identified key data transformations
- [ ] Did not get lost or backtrack excessively

## Friction Log

Record pain points during evaluation:

-
-
-
