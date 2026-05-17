# alloc-bounds — snipe repo review

run_id: ecebe5258308
scope: project

## Threat-model gate

snipe is a local CLI binary. Inputs:

- Local Go source files + go.mod (user-trusted)
- Local SQLite at `.snipe/index.db` (user-owned)
- stdin (`refs` batch, `edit` batch — user-driven)
- HTTPS responses from Voyage AI embeddings API (authenticated outbound; trusted-ish third party)

There is **no public network ingress** (no `http.HandleFunc`, no `net.Listen` outside tests). The alloc-bounds rule catalog targets DoS-via-attacker-allocation; for a single-user CLI most allocations sized by `len(localFile)` or `len(localQueryResult)` fall under the "internal config / bounded by trusted source" exception.

I walked every non-vendor, non-test site matching `make([]|make(map|make(chan|ReadAll|json.NewDecoder|bufio.NewScanner`. The Voyage-response sites are the only ones touching a remote boundary. They have small response shapes, an outer `http.Client.Timeout: 30s`, and authenticated endpoint pinning. The one stream that *does* lack a byte cap (`DownloadFile` body, consumed by `ParseBatchResults`) was a deliberate design choice — batch JSONL is multi-GB, line-bounded scan is the intended shape.

## Findings

None at action tier. Closest borderline candidate (`DownloadFile` error-path `io.ReadAll(resp.Body)` with no cap) is in an outbound client against a trusted authenticated API on a sad path that already returns to a user-driven CLI — capping at e.g. 64 KiB would clip useful error bodies without protecting anything an attacker can reach.

Zero findings is the honest call here. Re-run alloc-bounds if snipe ever exposes a server surface (LSP, daemon, MCP HTTP) — the threat model flips at that point and `internal/embed/batch.go:319` (Scanner.Buffer max=1 MiB on remote JSONL) plus the three `io.ReadAll(resp.Body)` error paths in `internal/embed/batch.go` and `internal/embed/client.go:183` become worth a second look.

trixi log-skill alloc-bounds findings 0 --run-id "ecebe5258308"
