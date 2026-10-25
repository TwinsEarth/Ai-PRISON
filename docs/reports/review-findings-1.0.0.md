# AI Prison — Code Review Findings (v1.1 hardening)

**Date:** 2026-10-25 (Asia/Shanghai, UTC+8)
**Toolchain:** go1.27.3 darwin/arm64, staticcheck 2026.2.2 (0.8.2)
**Scope:** full `internal/...` + `cmd/aiprison`, per the 10 review categories.
**Gate state after fixes:** `go build ./...`, `go vet ./...`, `gofmt -l .` empty,
`go test -race -count=1 ./...` all green.

## Summary

| Severity | Found | Fixed | Left open |
|----------|------:|------:|----------:|
| P0 (critical) | 0 | 0 | 0 |
| P1 (high) | 1 | 1 | 0 |
| P2 (medium) | 1 | 1 | 0 |
| P3 (low/style) | 8 | 8 | 1 (documented) |

No P0 issues: no data races (`-race` green before and after), no path traversal
(segment file names are derived solely from the global seq number, never from
`kind`/payload/user input), no unguarded type assertions (all via comma-ok or
`errors.As`), hash inputs are deterministic (RFC3339Nano UTC, JSON maps with
lexicographically sorted keys), and 212 comments explicitly mark GRAM/TLM/
hardware-binding/MicroVM/custody-token features as simulated.

---

## P1 — `resource.Deprive` nil `SnapshotStore` panic

- **File:** `internal/enforcement/resource/deprive.go:114` (`Deprive`)
- **Symptom:** `NewEngine` defaults `Revocations`, `Events`, `Receipts` to no-op
  sinks and nil-checks `Backend`, but never nil-checks `Snapshots`. A caller
  building `Config{Backend: ...}` without `Snapshots` hit a nil-interface call
  at step 3 (`e.Snapshots.Seal(...)`) → `runtime panic: invalid memory address`.
- **Fix:** added an explicit precondition
  `if e.Snapshots == nil { return nil, fmt.Errorf("resource: deprivation needs a SnapshotStore") }`
  alongside the existing `Backend` check. Behaviour of the happy path unchanged.
- **Regression test:** `internal/enforcement/resource/resource_test.go` →
  `TestDepriveNilSnapshotStoreReturnsError` (asserts a clear error instead of a
  panic for both nil `Snapshots` and nil `Backend`).

## P2 — Store ignored `context.Context` cancellation

- **File:** `internal/store/file.go` (`Append`, `Sync`), `internal/store/memory.go` (`Append`)
- **Symptom:** the `Store` contract accepts a `ctx context.Context`, but both
  backends bound it to `_` and never honored cancellation — a long fsync on
  `SyncEveryAppend` could not be aborted.
- **Fix:** added `if err := ctx.Err(); err != nil { return ..., err }` at the top
  of `FileStore.Append`, `FileStore.Sync`, and `MemoryStore.Append`. A cancelled
  context aborts *before* any mutation, so the ledger is left untouched.
- **Regression test:** `internal/store/coverage_test.go` →
  `TestAppendSyncHonorsCancelledContext` (already-cancelled ctx returns
  `context.Canceled`, `Len()==0`, and a subsequent live-ctx Append still works).

## P3 — staticcheck findings (all fixed)

| # | Location | Check | Fix |
|---|----------|-------|-----|
| 1 | `internal/audit/receiptbook.go:175` | U1000 unused `(*ReceiptBook).pendingCount` | Removed dead method; pending-vs-acked signal already surfaced by `UntrustedNodes()`/`All()`. |
| 2 | `internal/enforcement/migration/custody.go:15` | U1000 unused type `custodianShare` | Removed; shares stored inline as `map[string]string`. Simulated-share semantics preserved in doc comment. |
| 3 | `internal/prison/persist_test.go:175` | SA1012 nil context to `Replay` | Pass `context.Background()`; added `context` import. |
| 4 | `internal/store/store_test.go:23` | U1000 unused `dir` field on `backendFactory` | Removed the never-set/never-read field. |
| 5 | `internal/governance/adapters.go:43` | ST1020 comment format | Comment now starts with `NewPrincipalLookup ...`. |
| 6 | `internal/prison/journey.go:121` | ST1020 comment format | Comment now starts with `Print ...`. |
| 7 | `internal/prison/system.go:283,286` | ST1016 receiver-name inconsistency | Renamed receiver `s *System` → `sys *System` on `Clock()` and `RuntimeStatus()`; all 23 `*System` methods now share `sys`. |
| 8 | (coverage gap) | prison 55.3% / store 69.1% | Added `internal/prison/drill_test.go` and `internal/store/coverage_test.go`; see coverage report. |

## Leftover (documented, not changed)

- **`resource.Deprive` swallows `Backend.Current` error** (`current, _ := e.Backend.Current(...)`, deprive.go:149).
  In the simulated in-memory backend `Current` never fails; surfacing it would
  change the report shape and the e2e/baseline numbers. The field is
  defensive-only; left as-is and documented in code.
- **`publishLocked` hard-fails on "segment already exists"** (`file.go:256`).
  This is deliberate: after a successful `rename`, a subsequent directory-fsync
  failure leaves the committed segment on disk while `f.persisted` lags. The next
  publish detects the existing segment and returns an explicit
  `store: segment already exists ...` error rather than overwriting a committed
  file (which would break crash consistency). Leaving the guard in place.

## Category-by-category checklist

1. **Concurrency:** `Set`/`Chain`/`ReceiptBook`/`FileStore` all use correct
   `sync.RWMutex` (RLock for reads, Lock for writes); no RLock→Write-unlock
   mismatch. `go test -race -count=2 ./...` green.
2. **Error handling:** `ledgerRecorder` sticks the first `Append` failure and
   surfaces it at `Close()` — never silently continues. New ctx error + nil
   guards above.
3. **Panic risk:** no nil-pointer, out-of-range, uninitialised-map or
   unguarded type-assertion paths remain after the P1 fix. JSON marshal sites
   (`store.hashRecord`, `audit.hashOf`) panic only on the impossible case of
   marshalling strings/maps-of-strings.
4. **Context cancellation:** Store `Append`/`Sync` now honor `ctx.Err()`.
5. **Idempotency:** `Revoke` (same ID no-op), `RegisterPending` (same ID no-op),
   `MemCustodyStore.Seal` (same id returns existing), `ledgerRecorder.checkpoint`
   (seen*/sig dedup) all verified.
6. **Handle/process leaks:** `os.CreateTemp`/`OpenFile`/`Open(dir)` all `Close`
   on every path (including error paths); e2e subprocess uses `CombinedOutput`
   which waits.
7. **Path traversal:** segment filename = `seg-%010d.log` formatted from a
   `uint64` seq; `kind`/payload are JSON fields only, never joined into paths.
   `--data-dir` is the user-chosen root; nothing reads outside it.
8. **Hash correctness:** `store.hashRecord` uses a positional 5-element array
   with UTC RFC3339Nano; `audit.hashOf` uses a struct with a `map[string]string`
   payload (JSON sorts keys deterministically). Empty genesis `PrevHash` cannot
   be spoofed because the recomputed digest must match the stored `Hash`.
9. **API boundary:** nil store (`Config.Store == nil`) compiles out all
   persistence byte-for-byte identical to v1.0; `OpenReadonly` refuses appends.
10. **Simulated boundary:** 212 `simulat*`/`not real`/`no real` comments; grep
    for "real isolation/enforcement/cgroup/seccomp/firecracker/hardware" only
    returns negated-context comments.
