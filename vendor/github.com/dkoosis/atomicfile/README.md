# atomicfile

Atomic, crash-durable file replacement for Go — a thin wrapper over
[google/renameio](https://github.com/google/renameio) that adds the two
guarantees it omits: the parent-directory fsync that makes the rename itself
durable, and `F_FULLFSYNC` on macOS, where plain fsync does not reach stable
storage.

```go
err := atomicfile.WriteFile("state.json", data, 0o644)

err := atomicfile.WriteFileFunc("big.log", 0o644, func(w io.Writer) error {
    return render(w) // streamed body
})
```

The package documentation carries the full motivation and — read it — the
three cases where rename-replace is the **wrong** primitive: lock-files,
live SQLite databases, and multi-writer read-modify-write files.

Born from a nine-repo defect audit (2026-08) that found ~40 shipped bugs of
this class. Enforced fleet-wide by a ruleguard rule that flags raw
`os.WriteFile` on durable state.
