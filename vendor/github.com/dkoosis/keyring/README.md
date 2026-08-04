# keyring

Keychain/Secret Service access for Go — the macOS keychain via `security`,
the Linux Secret Service via `secret-tool` — no cgo, no third-party
dependencies. Built for CLI tools and daemons that need an API key or token
without config-file or env-var plumbing.

```go
store, err := keyring.New("myapp")
if err != nil { ... }

// Keychain-first, env fallback (works headless, works on Linux — see the
// Linux section below for what "works" means on a headless Linux box).
key, err := store.GetOrEnv("anthropic", "MYAPP_ANTHROPIC_API_KEY")
```

## Guarantees

1. **Secrets never touch argv.** Writes pipe the value to
   `security add-generic-password` on stdin, so it can never appear in `ps`
   or a process-table snapshot.
2. **Writes are verified.** A locked keychain can make `security` report
   success while storing nothing; `Set` reads the value back and compares.
3. **Absolute binary path.** `/usr/bin/security` is invoked absolutely — a
   hijacked `$PATH` cannot substitute a malicious binary into the credential
   path.
4. **"Not found" is never conflated with "could not read".** Only
   `security`'s confirmed item-not-found (exit 44) maps to `ErrNotFound`.
   A locked keychain, a denied dialog, or a timeout is `ErrUnreadable` —
   callers using `Has` as an overwrite guard must block on it, because
   treating "couldn't read" as "absent" invites clobbering a value that is
   actually there.
5. **Bounded calls.** Every `security` invocation carries a timeout
   (default 10s, `WithTimeout` to change), so a wedged unlock prompt becomes
   an error instead of a hang.

The not-found/unreadable split is a best-effort classification of the Darwin
`security` CLI (exit status + stderr text). It is reliable on stock macOS,
but it is a CLI heuristic, not an OS guarantee.

## The CLI

`go install github.com/dkoosis/keyring/cmd/keyring@latest` — the same
guarantees as the library (it IS the library underneath), no `security(1)`
incantation anywhere. Every failure message ends with the next command to
run.

**First-time setup** — store a key, prove it landed:

```sh
keyring set myapp anthropic     # hidden prompt; strips a pasted trailing newline
keyring get myapp anthropic     # masked receipt; --raw to reveal
```

**"My app can't find its key":**

```sh
keyring doctor myapp
```

Doctor names what's wrong — missing item, locked keychain, duplicate items,
a stale env var shadowing the keychain, a value with a pasted newline — and
prints the exact fix for each. With a `keyring.json` manifest in the repo it
also diffs expected vs. stored accounts and tells you where to obtain each
missing credential. `--fix` applies the safe repairs after a per-item
confirm.

**"I think my key is wrong":**

```sh
keyring get myapp anthropic --raw   # compare with the source of truth
keyring set myapp anthropic --force # overwrite; read-back verified
```

**Cleaning up legacy items** (old `myapp-anthropic`-style names, duplicates,
pasted newlines) — plan first, nothing applies before one confirmation:

```sh
keyring migrate myapp
```

**Agent-driven setup** — no TTY, everything from exit code + `--json`:

```sh
printf %s "$KEY" | keyring set myapp anthropic --stdin --json  # verified:true
keyring doctor myapp --json                                    # exit 8 → findings[]
keyring doctor myapp --fix --yes --json                        # heal, receipts
```

Exit codes are stable and map 1:1 to the library sentinels:
`0` ok · `2` validation · `3` not found · `4` unreadable/locked ·
`5` verify failed · `6` already exists · `7` unsupported/disabled ·
`8` doctor/migrate found unresolved problems.

Destructive actions (`rm`, `--fix`, `migrate`) confirm on a terminal and
fail closed without `--yes` when piped — an agent can never delete or
rewrite an item by accident.

## Set vs. SetIfAbsent

`Set` writes with `-U` (update-if-exists) — it always succeeds against an
existing item, silently overwriting it. A concurrent writer landing between
another caller's presence check (`Has`) and its own `Set` is clobbered with
no error to either side; this package has no compare-and-swap, and `Has` is
advisory only (see Guarantees #4). Callers that need cross-process
integrity — no two writers racing to the same account — must either
serialize writes per (service, account) themselves, or use `SetIfAbsent`.

`SetIfAbsent` is the write-once primitive: it skips `-U`, so
`add-generic-password` fails with a confirmed duplicate-item error
(`errSecDuplicateItem`, exit 45) against an existing item instead of
overwriting it. That failure maps to `ErrExists`; on confirmed absence it
stores and read-back verifies exactly like `Set`. Use it for bootstrap or
token-refresh flows where two processes might race to initialize the same
account and only one write should win — the loser gets `ErrExists`, never a
clobber.

## Linux

Linux uses `secret-tool` (the libsecret CLI) instead of `security` — same
shape (shell out, secret on stdin, absolute binary path), same sentinel
errors, but two things are weaker than the Darwin backend and worth reading
before you rely on this in production:

1. **The not-found/unreadable split is a cruder heuristic here.**
   `secret-tool` has no equivalent of `security`'s dedicated exit-44 code —
   it exits `1` for BOTH "no such secret" and any real failure (D-Bus
   unreachable, locked collection, denied access). The only signal
   distinguishing them is stderr: libsecret's CLI prints nothing on
   confirmed absence and an error message on every other failure. This
   package treats "exit 1, empty stderr" as `ErrNotFound` and everything
   else as `ErrUnreadable` — reliable on a normal desktop session, but a
   future libsecret release that starts printing on the not-found path
   would silently reclassify it as `ErrUnreadable`, never the other way
   around (see `notFoundExitStderrEmpty`'s doc comment).
2. **`SetIfAbsent` is check-then-act, not atomic.** `secret-tool store`
   always overwrites — there's no CLI primitive that fails on an existing
   item the way `security add-generic-password` (without `-U`) does. The
   Linux backend does a `lookup` first and only stores on confirmed
   absence, which closes the common bootstrap/token-refresh race but is a
   narrower guarantee than Darwin's `ErrExists` (see `writeIfAbsent`'s doc
   comment).

**`Supported()` needs a live Secret Service, not just a compiled backend —
this is the important one for headless boxes.** A CI runner, a cron job, or
a bare Linux server with no desktop session has no D-Bus session bus and no
keyring daemon (gnome-keyring, kwallet) listening, even though the Linux
backend is compiled in. `Supported()` returns `true` only when BOTH hold:

- `secret-tool` exists at `/usr/bin/secret-tool` (override the lookup path
  with `WithSecurityBin` if your distro installs it elsewhere — Linux, unlike
  macOS, has no single guaranteed location).
- `$DBUS_SESSION_BUS_ADDRESS` is set (the standard signal a session bus is
  reachable).

Neither check executes `secret-tool` itself, so this is a best-effort,
non-authoritative probe, not a live round-trip to the Secret Service — a
stale or misleading env var could still pass it. On a genuinely headless
box (no session, no `DBUS_SESSION_BUS_ADDRESS`) it reliably reads
**backend-absent**, which is the case that matters: `GetOrEnv` falls
through to the environment exactly as it does on the `!darwin,!linux` stub,
so a headless consumer (e.g. a token stored off `EnvironmentFile` instead of
the keyring) gets the same one-code-path behavior as any other unsupported
platform. `ErrUnreadable` never falls through on any platform — a locked
keychain, or a Linux box that has a session but a denied/locked collection,
surfaces as an error rather than silently downgrading to env.

List/DumpItems/DumpDuplicates return `ErrUnsupported` on Linux: `secret-tool`
has no clean "every item under one service" enumeration the way Darwin's
`dump-keychain` does without a schema — deliberately out of scope for this
backend.

## Non-darwin, non-linux

On every other build `Supported()` returns false and every keychain
operation returns `ErrUnsupported`. `GetOrEnv` falls through to the
environment there, so one code path serves every platform.

## Conventions

- **service** = your app's name (`myapp`) — the keychain namespace.
- **account** = the secret's purpose (`anthropic`, `github`).
- **env fallback** = `<APP>_<PROVIDER>_API_KEY` (`MYAPP_ANTHROPIC_API_KEY`).

Store a secret from a terminal (value prompted hidden, off argv):

```sh
keyring set myapp anthropic
```

## Single-item precondition

`Set` writes with `add-generic-password -U -s <service> -a <account>` and
verifies with `find-generic-password -s <service> -a <account>`. `-U`
updates the item matching that attribute set; `find` returns the first
service+account match in keychain search order. Both target the *same*
item only if exactly one exists.

**This is a hard precondition, not a caveat.** If a duplicate (service,
account) item exists — planted by another tool with extra attributes, or
living in a different keychain (system vs. login) — write and read-back
can silently address *different* items:

- Get can return the OTHER item's value: stale, or attacker-controlled if
  the higher-priority keychain isn't yours to trust.
- Set's read-back can verify against the other item too, masking a write
  that landed on the wrong one — a correct write followed by a read of the
  other item produces a spurious `ErrVerifyFailed`, or (if the values
  happen to match) a masked no-op.

A caller MUST do one of the two:

1. **Pin a keychain** with `keyring.New(service, keyring.WithKeychain(path))`
   — every find/add call scopes to that one keychain file, closing the
   ambiguity outright. `path` must be absolute.
2. **Guarantee uniqueness** — exactly one (service, account) item across the
   whole default keychain search path, for the life of the Store.

This library assumes it is the only writer for its service namespace. If
`ErrVerifyFailed` fires on what looks like a correct write and you have not
pinned a keychain, check for a duplicate item first (`keyring doctor
<service>` flags duplicate groups across the search list) — the fix is
`keyring migrate <service>` or `--fix` to dedupe, pinning a keychain, or
picking a distinct service name, not retrying the write.

## Test kill-switch

`KEYRING_DISABLE=1` (any non-empty value) makes every operation return
`ErrUnsupported` and `Supported()` report false — the Store behaves exactly
like a build with no backend, and `GetOrEnv` falls through to the
environment. It exists for test harnesses that exec a **built** consumer
binary (blackbox/txtar suites), where `WithSecurityBin` cannot be injected:
set it in the subprocess env and the developer's real keychain can never
leak into an env-isolated test. Read at call time, so in-process tests can
toggle it with `t.Setenv(keyring.DisableEnv, "1")`.

## Consuming this module (it is private)

`github.com/dkoosis/keyring` is a private repo. Builds that fetch it need:

```sh
export GOPRIVATE='github.com/dkoosis/*'
# plus a github.com git credential (gh auth login covers local dev)
```

CI (GitHub Actions) — the default `GITHUB_TOKEN` cannot read another
private repo. Add a fine-grained PAT (read-only Contents on this repo) as a
repo secret, e.g. `DKOOSIS_MODULES_TOKEN`, then before any Go step:

```yaml
- run: git config --global url."https://x-access-token:${{ secrets.DKOOSIS_MODULES_TOKEN }}@github.com/".insteadOf "https://github.com/"
  env: {}
- run: echo 'GOPRIVATE=github.com/dkoosis/*' >> "$GITHUB_ENV"
```

Docker builds need the same via a build secret — never bake the token into
a layer.

## Testing

`go test ./...` runs stub-based contract tests (argv shape, stdin protocol,
error classification, timeouts) on any OS. The live end-to-end test against
the real keychain is opt-in — `KEYRING_LIVE_E2E=1 go test -run
TestLiveKeychain` in an interactive terminal (`security`(1) prompts on
`/dev/tty`, so it cannot run headless). CI runs contract tests only; a
manual live check on a real Mac gates each release.
