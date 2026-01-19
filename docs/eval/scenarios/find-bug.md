# Scenario: Find Bug

Debug a failing test or runtime error using snipe.

## Setup

Provide Claude with:
- Error message or stack trace
- Failing test output
- File path where error surfaces (if known)

Example prompt:
> "This test is failing with `unexpected nil pointer`. Use snipe to find the root cause."
> ```
> --- FAIL: TestProcessRequest (0.00s)
>     handler_test.go:45: got nil, want valid response
> ```

## Goal

Locate the root cause of the bug using snipe's navigation commands.

## Expected Commands

| Command | Purpose |
|---------|---------|
| `def` | Jump to function definitions mentioned in error |
| `callers` | Find what calls the failing function |
| `refs` | Track where a variable is used |

## Success Criteria

- [ ] Identified the correct function/line causing the issue
- [ ] Explanation traces back to root cause (not just symptom)
- [ ] Did not require reading entire files to find it

## Friction Log

Record pain points during evaluation:

-
-
-
