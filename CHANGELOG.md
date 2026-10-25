# Changelog

All notable changes to AI Prison are documented in this file. The format is
loosely based on [Keep a Changelog](https://keepachangelog.com/), and the
project follows [semver](https://semver.org/).

## [Unreleased]

## [0.8.0]

### Added
- `internal/enforcement/migration`: the migration-suppression layer (迁移抑制层),
  per docs/02-technical-route.md §4. The goal is NOT to stop migration (impossible
  in open weights) but to make a migrated copy worthless: capability unusable,
  reputation non-inheritable, collaboration state unrecoverable. Three paths, all
  interface + in-memory/simulated:
  - **Hardware fingerprint binding (硬件绑定)** — `MemHardwareBinder` binds
    capability credentials to a simulated `domain.Agent.HardwareFingerprint`
    string. `Validate` runs the REAL credential order first (revocation set →
    local state → validity window via `domain.Credential.IsUsable`) and then
    requires the present fingerprint to match; a copy on another machine fails.
    Applied at L3+. Explicitly simulated: only deters casual copying, NOT an
    unbreakable hardware root of trust.
  - **Portable work-record anchor key (可迁移工作记录的锚定密钥)** —
    `MemRecordKeeper` anchors an agent's lessons/results/reputation to a
    simulated key id. `Portable(WorkRecord)` is true only while the anchor key
    is live; L5 revokes it, so a migrated copy carries no inherited
    reputation/history (aligns with L5 `ZeroReputation`).
  - **Multi-party snapshot custody (快照多方加密封存)** — `MemCustodyStore`
    seals a pack_diff snapshot behind a simulated M-of-N across N custodians.
    Unseal needs ≥ M distinct valid custodian shares; below the threshold it
    fails. Every seal AND unseal attempt is chained (`EventSnapshotSealed`).
    Explicitly simulated: NOT real Shamir / threshold cryptography or key custody.
  - **Orchestration** — `Service.SuppressMigration(agentID, level)` applies the
    level-profile-mandated combination: hardware binding at L3+, record-key
    revocation + snapshot custody at L5. Idempotent and race-safe.

### Notes
- Everything is an interface + in-memory simulation: no real hardware root of
  trust, no real threshold crypto / multi-party key custody, no real key
  ecosystem. The package is strictly defensive (isolation/audit/accountability).

## [0.7.0]

### Added
- `internal/governance`: the governance layer (治理层) — violation detection →
  seven-stage adjudication → sentence generation → appeal intake →
  rehabilitation assessment, plus the unified sentence state machine, per
  docs/04-governance.md and docs/governance-templates/committee-charter.md.
  - **Detection & case opening (立案)** — `OpenCase(domain.Violation)` treats a
    detection as a FILING BASIS only (not a sentence), reads the baseline from
    `Violation.RecommendedLevel()`, runs the stage-① signal-integrity gate
    (known type + non-empty signals) and opens the case at `open`/待裁定.
  - **Seven-stage pipeline + two-phase consent (七阶段 + 两阶段同意)** — stages
    align exactly with committee-charter.md §4.1: ① 立案与信号核验,
    ② 证据与先例审查, ③ 分级基线确认 (+ five-seat committee formation),
    ④ 第一阶段表决 deprive, ⑤ 第二阶段表决 grade, ⑥ 异议记录与判决生成,
    ⑦ 申诉/矫正复核窗口. Stages cannot be skipped. Phase 1 uses
    `domain.TallyPhase(PhaseDeprive)` strict majority; failure dismisses the case
    with NO deprivation. Phase 2 uses `TallyPhase(PhaseGrade)`; no majority
    leaves the case deferred with no definite-level sentence. Votes are
    append-only — dissenting votes and their Reason are recorded and the package
    exposes NO edit/delete method.
  - **Human-accountability gate (人类问责耦合)** — `IssueSentence` reuses
    `identity.ValidateAccountability` and REFUSES a sentence without at least one
    well-formed human record. Output is a `domain.Sentence(status=approved)`
    carrying charges, the full immutable vote book, level deprivations,
    accountability and timestamps.
  - **Unified state machine** — a legal-transition table over
    `domain.SentenceStatus` (proposed→approved→enforcing→appealed/suspended→
    completed/upgraded/overturned); illegal moves (e.g. proposed→completed,
    restoring an L5 identity) are rejected. Every transition appends a governance
    event (`EventAdjudicated`/`EventSentenced`/`EventAppealFiled`/
    `EventAppealRuled`/`EventRehabilitationEvaluated`/`EventRestored`) to the
    injected `EventSink` (real `*audit.Chain` adapter in `adapters.go`).
  - **Appeal (申诉)** — only the agent's human principal may file
    (`FileAppeal`, backed by a `PrincipalResolver` / `RegisterAgent`). Suspension
    strictly follows `level.go`: L0–L3 → `suspended`, L4–L5 stays `enforcing`.
    An upheld L0–L4 appeal produces a `RestorationOrder`; **L5
    (`CanRecover()==false`) can never produce one** — the record may be corrected
    but the identity is never unsealed.
  - **Rehabilitation (矫正评估)** — supervised behaviour-distribution scoring
    (injectable `RehabResult{Score,Threshold,Passed}`) for L2–L3: pass →
    `completed` + restoration order; fail → escalate exactly one level upward via
    `Level.CanEscalateTo`, capped at L5.
  - Dependencies stay standard-library-only (go.mod unchanged); the package uses
    small injected interfaces (`EventSink`, `PrincipalResolver`) with real
    adapters for `*audit.Chain`. Pure in-memory simulation of governance decisions
    and the state machine — NOT a real legal process, no real committee and no
    judicial effect; strictly defensive.

## [0.6.0]

### Added
- `internal/enforcement/identity`: the identity-and-communication layer (身份与
  通信层), per docs/02-technical-route.md section 5.
  - **DID / AgentCard revocation** — `Service.RevokeIdentity` writes BOTH the
    identity credential (`identity:<agent>`) and the communication credential
    (`communication:<agent>`) into the authoritative global revocation set
    (`internal/revocation`) and flips `AgentCard.Revoked` / agent status to
    `identity_revoked`. The real `revocation.Set` is reused as the
    `domain.RevocationChecker`, so a still-in-window credential is rejected by
    `domain.Credential.IsUsable` (revocation checked before expiry/state) —
    the "revoked but not expired" rule is not re-implemented.
  - **Communication cut + forced receipts + collateral group freeze
    (回执强制 / 连带冻结)** — an in-memory `NetworkTopology` models node↔agent
    placement. `CutCommunication` registers one pending
    `communication.cut` receipt on every node carrying the channel; untrusted
    judgment is delegated to the audit receipt book
    (`ReceiptBook.UntrustedNodes`, pending/missing = untrusted — the single
    source of truth, not a second un-acked set). A node that never acks is
    untrusted and EVERY agent on it — not only the sentenced one — receives a
    `FreezeOrder` (`GroupFreeze=true` for collateral agents). "Broadcast" is
    never treated as "delivered". `AckNode` acknowledges and re-projects.
  - **Human accountability coupling (人类问责耦合)** — `DetermineAccountability`
    maps within-authorisation → principal joint liability, out-of-authorisation
    → deployer monitoring-duty review, and dual-use-knowledge exploitation →
    additive model-provider capability-control-duty review.
    `ValidateAccountability` enforces the mandatory invariant: every identity
    revocation MUST carry at least one well-formed human record, otherwise the
    disposition is refused and the revocation set is left untouched.
  - Dependencies are small injected sinks (`RevocationSink` / `EventSink` /
    `ReceiptController` / `NetworkTopology`) with real adapters in
    `adapters.go` wiring `*revocation.Set`, `*audit.Chain`, `*audit.ReceiptBook`
    — the same DI style as `internal/enforcement/resource`.
- Unit tests: revoked-but-not-expired credentials rejected via the real set +
  card revoked; all-ack → no untrusted node / no freeze; partial ack → silent
  node untrusted and its innocent co-located agent collateral-frozen; idempotent
  revocation (no duplicate entries); the three accountability rulings mapped to
  principal/deployer/model-provider; a missing-accountability disposition
  refused by the invariant gate; and concurrent revoke/cut/ack (-race).

### Changed
- No wire/contract break: reuses `domain.EventIdentityRevoked`,
  `domain.EventNodeMarkedUntrusted`, `domain.EventReceiptAcked`,
  `domain.CredentialIdentity` / `domain.CredentialCommunication` and the
  existing revocation/receipt-book read/write APIs; no schema or domain field
  semantics changed.

### Simulation scope
- Everything is in-memory, simulated and not a real identity/communication
  control: no real DID network, no real message bus / PMB, no real node
  communication. Topology, credentials, receipts and freeze orders live in
  this process only. Strictly defensive (isolation, audit, accountability) —
  no escape, attack, weaponization or credential-forgiveness content.

## [0.5.0]

### Added
- `internal/enforcement/isolation`: the isolation gradient (隔离层).
  - `Backend` interface with four simulated backends matching the design
    document's figures: Wasmtime (3–46ms, linear-memory / language-level
    isolation, L0–L1 daily monitoring), Docker (~500ms, namespace+cgroup,
    L1–L2), Firecracker MicroVM (~125ms, KVM hardware isolation / independent
    kernel, L3) and Full VM (seconds, full guest OS, L3–L5). Every backend
    exposes `Simulated() bool == true`.
  - `Gradient`: `Recommended(level)` is derived from the authoritative
    `LevelProfile.Sandbox` field (no drift with `internal/domain/level.go`),
    and `EscalationPath` moves strictly stepwise on the lightest→heaviest
    continuum (e.g. Wasmtime→MicroVM passes through Docker; downgrade in place
    rejected). Not every agent is put in the heaviest sandbox.
  - `Monitor`: consumes defensive syscall / network / file-access signals and
    freezes the agent DURING the anomaly stream — `Observe` fires the freeze
    hook and the `EventIsolated` event the moment cumulative severity crosses
    threshold, before the rest of the sequence is consumed
    (`FreezeSignalOrdinal` makes the mid-stream freeze assertable).
  - `Controller` migration is establish-before-cut: `Establish` creates the new
    controlled environment while the old one stays live; `ConfirmReady`
    releases the old environment only after the new one is confirmed ready — no
    cut-over window and no state loss mid-switch.
- Unit tests: level→backend mapping against the level profiles, stepwise
  escalation with downgrade rejection, mid-stream freeze ordinal assertion,
  establish-before-cut ordering, simulated/latency labels on every backend,
  and concurrent migrations + monitoring (-race).

### Changed
- No wire/contract break: freeze reuses `domain.EventIsolated` /
  `EventMigrationSuppressed` and `domain.SandboxBackend` labels; no schema
  changes.

### Security
- In-memory simulation only: no real Wasmtime/Docker/Firecracker/VM runtime, no
  KVM or hardware isolation, start delays are recorded not waited on, and the
  signal categories carry no exploit detail. Defensive scope unchanged.

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
