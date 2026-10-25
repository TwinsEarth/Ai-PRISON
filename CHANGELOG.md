# Changelog

All notable changes to AI Prison are documented in this file. The format is
loosely based on [Keep a Changelog](https://keepachangelog.com/), and the
project follows [semver](https://semver.org/).

## [Unreleased]

## [0.4.0]

### Added
- `internal/enforcement/resource`: the resource-deprivation layer (资源剥夺层).
  - `Quota` as a *revocable* resource credential (CPU millis / memory / storage
    / network / frozen pages), not a static config, and a `Backend` interface
    (`Allocate` / `Current` / `StopAllocation` / `PlanReclaim` / `ReclaimStep` /
    `FreezeMemoryPages` / `CutNetwork`) with the in-memory `InMemoryBackend`.
    `Allocate` consults the global revocation set FIRST — revoked-but-not-expired
    credentials are rejected — and post-deprivation `StopAllocation` blocks new
    work. `ReclaimStep` walks the CPU quota down through fixed rungs (default 4:
    1000→750→500→250→0), then freezes pages and cuts the network: never an
    instant zero, idempotent after the quota hits zero.
  - `Engine.Deprive` orchestrates the design's five-step sequence in the
    assertionable order: write revocation → stop new allocation → seal the
    pack_diff snapshot on the LIVE pre-reclaim environment (hash anchored on the
    chain via `EventSnapshotSealed`) → gradual reclaim rungs + page freeze +
    network cut → forced per-node receipts. `DepriveReport` exposes the step
    list, the full CPU rung history, the snapshot and the open acknowledgements;
    non-acknowledged nodes are judged untrusted by `audit.ReceiptBook` (this
    package does not reimplement the v0.6 group freeze).
  - In-memory `MemSnapshotStore` (one evidence snapshot per agent+sentence,
    idempotent re-run) plus dependency-injection adapters wiring the real
    `revocation.Set`, `audit.Chain` and `audit.ReceiptBook`.
- Unit tests: post-revocation allocation rejection; gradual rung history with
  strict intermediates and exact final zero; snapshot-before-reclaim ordering
  with chain verification; untrusted-node exposure through the real receipt
  book; idempotent re-run (single revocation entry, single snapshot, quota not
  re-walked); concurrent deprive/allocate (-race).

### Changed
- No wire/contract break: events reuse `domain.Event*` constants, receipts
  reuse `domain.Receipt` / `audit.OpSnapshotSeal`, and the revocation entry
  reuses `internal/revocation.Entry`; no schema changes.

### Security
- In-memory simulation only: no real cgroup/quota control, no real memory
  freeze or network cut, no real encrypted snapshot custody. Defensive scope
  unchanged (traceability, verifiability, accountability only).

## [0.3.0]

### Added
- `internal/audit`: the audit layer.
  - Tamper-evident judgement hash chain (`Chain`): `Append` stamps index,
    RFC3339Nano UTC timestamp, prevHash and a SHA-256 digest over the
    canonical, key-sorted event representation; the head links to the fixed
    `GenesisPrevHash`. `Get` / `Len` / `Entries` expose detached copies;
    `Verify` (and the stateless `VerifyEntries`) re-derives every digest and
    link and reports the offending index on any tamper.
  - `ReceiptBook`: broadcast receipts are never treated as delivered.
    `RegisterPending`, `Ack` (stamps a simulated HMAC-SHA256 signature over
    operation + before/after state hashes, ackedAt, status=acked),
    `MarkMissing`, and `UntrustedNodes` — the nodes holding any non-acked
    receipt, which v0.6 will use for the group freeze (连带冻结).
  - `Report(sentenceID)` returns a `contracts.ComplianceReport`: per-hop
    compliance (every step acknowledged) contrasted with end-to-end
    compliance (per-hop AND the final snapshot-seal receipt acked), plus
    `pendingReceipts` and `untrustedNodes` counts.
- Unit tests: chain linking and verification, tamper detection for
  type/payload/prevHash/timestamp/hash with index location, attacker-recomputed
  digests detected as broken links, concurrent appends (-race), simulated
  signature sensitivity to key/operation/state, pending→acked→missing
  lifecycle, untrusted-node set, and all four per-hop/end-to-end combinations.

### Changed
- No wire/contract break: the chain reuses `domain.Event`, receipts reuse
  `domain.Receipt`, and the report reuses `contracts.ComplianceReport`; no
  schema changes required.

### Security
- Simulated, in-memory only: the "node signatures" are HMAC values with no
  real PKI or node identity, and the chain lives in one process. Defensive
  scope unchanged (traceability, verifiability, accountability only).

## [0.2.0]

### Added
- `internal/revocation`: the global, append-only, idempotent revocation set
  (`Set`, `Entry` aligned with `schemas/revocation-entry.schema.json`).
  - `Set.Revoke` / `IsRevoked` implement `domain.RevocationChecker`: revocation
    is consulted before state and before expiry by
    `domain.Credential.IsUsable`. Writes are idempotent and there is no remove
    path — a revoked credential stays revoked for the life of the set.
  - Simulated cross-node acknowledgement: `Revoke(entry, holdingNodes)`,
    `AckNode`, `UnacknowledgedNodes`; `enforced` flips true only once every
    holding node has confirmed (empty holding list = immediately enforced).
    `UnacknowledgedNodes` is the input the v0.6 group-freeze (连带冻结) flow
    will consume.
  - `List` / `Get` export detached, write-ordered snapshots for simulated
    cross-node replication.
- Unit tests: revoked-but-not-expired rejection through the real set,
  idempotent re-revoke, enforced-only-after-full-ack, empty-node-list immediate
  enforcement, unknown-credential acks, concurrent Revoke/IsRevoked (-race)
  and ordered snapshot semantics.

### Changed
- No wire/contract break: `Entry` reuses existing domain types and the
  existing revocation-entry schema; no schema changes required.

### Security
- In-memory/simulated only: no real network propagation, key ceremony or
  enforcement backend. Defensive scope unchanged.

## [0.1.0]

### Added
- Repository foundation: Go 1.27 module (standard-library only, zero
  third-party dependencies), MIT license, Makefile, `.gitignore`,
  contributing/security/conduct files.
- `internal/domain` core model:
  - `Agent`, `AgentID`, `DID`, `AgentCard`, agent lifecycle statuses.
  - `Level` L0–L5 with immutable profiles (deprivations, applicable
    behaviours, duration class, permanence, recovery, appeal suspension,
    sandbox posture, reputation effects) and JSON ("L0"…"L5") round-trip.
  - `Violation` with a type → recommended-level grading table.
  - `Credential` with the mandatory validation order (revocation set before
    state before validity window) via a `RevocationChecker` interface.
  - `Sentence`, `Charge`, `Vote`, two-phase consent (`deprive`, `grade`).
  - `Event`, `Receipt`, `AccountabilityRecord`, `ReputationEvent`.
- `pkg/contracts`: public, JSON-schema-aligned wire contract with envelope
  and request/response types.
- `schemas/`: JSON Schema (draft 2020-12) for agent card, violation,
  sentence, accountability record, credential, revocation entry, event,
  receipt and compliance report.
- `cmd/aiprison`: `version` and `levels` commands.
- Documentation: README, roadmap, source design archive, and the full docs
  suite (product design, technical route, architecture, governance, threat
  model, recovery drills, references) with Mermaid diagrams.
- Domain unit tests covering level semantics, escalation, grading,
  reputation, committee tally and the revocation-before-expiry rule.

### Security
- Defensive scope only; no offensive or weaponization content.
- Every sentence type requires at least one human accountability record.
