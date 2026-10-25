// Package store is the durable governance ledger for AI Prison.
//
// It defines a single append-only Store contract shared by two backends:
//
//   - MemoryStore keeps the ledger in the process (the original simulation
//     substrate, useful for tests and the in-memory demo).
//   - FileStore is a REAL on-disk, append-only JSONL event log with a
//     monotonic sequence number and a per-record previous-hash chain. It
//     persists the revocation set, cases/sentences, audit events, receipts,
//     snapshot metadata and the audit hash chain across process restarts.
//
// Both backends satisfy the same contract and the same conformance test suite
// runs against both (table-driven). The ledger is event-sourced: state is
// rebuilt by replaying records in sequence order, and every replay re-verifies
// the per-record hash chain.
//
// # Durability and crash semantics (FileStore)
//
// Records are published in immutable segment files. A segment is built in a
// temporary file, fsync'ed, then atomically renamed over its final name, and
// the directory is fsync'ed (os.CreateTemp + f.Sync + os.Rename + dir fsync).
// Consequently:
//
//   - A published segment is always complete; a crash before the rename leaves
//     only an unpublished temporary file, which is discarded on open.
//   - A torn tail (a partial, non-parseable final line in a published segment)
//     is detected and reported; it may be repaired only by removing the
//     incomplete tail, never by guessing bytes.
//   - A tampered, otherwise complete record (bad content hash or broken link)
//     is a hard error that open/replay never silently ignores.
//
// Sync is the durability boundary: Append records into the active segment and
// Sync publishes it atomically. With SyncEveryAppend enabled (the default for
// the governance ledger) each Append is individually durable.
//
// # Scope and boundary
//
// This is REAL persistence for the governance/audit record: it durably stores
// decisions, revocations, receipts and evidence metadata, and detects
// tampering. It is NOT a real enforcement backend. Sandboxes, cgroups, real
// revocation distribution, hardware binding, GRAM/TLM and capability removal
// remain interfaces / in-memory simulations (see the enforcement packages).
// The ledger records what those simulated components did; it does not make
// those controls real.
//
// The package is strictly defensive: it provides traceability, verifiability
// and accountable recovery — no escape, attack or weaponization.
package store
