# Changelog

All notable changes to AI Prison are documented in this file. The format is
loosely based on [Keep a Changelog](https://keepachangelog.com/), and the
project follows [semver](https://semver.org/).

## [1.0.0]

v1.0.0 is the first stable release of the defensive AI-agent governance stack. On top of the
runnable L0–L5 recovery drills, it turns the project from an in-memory demo into a real, deployable
artifact by adding three things:

1. **Real, durable persistence of the governance ledger** — an append-only, per-record-hash-chained
   JSONL event store with crash-safe segment rotation (`os.CreateTemp` + `fsync` + atomic `rename`
   + directory fsync), event-sourced replay that rebuilds all governance state, and a unified
   `Store` interface with one table-driven suite running the same consistency/concurrency/crash/
   tamper tests against BOTH the memory and file backends.
2. **A real, deployable binary** — `make build` produces a stripped `-trimpath bin/aiprison` with
   link-time version/commit/build-time injection, and a cross-process smoke script proves
   demo → exit → restart → inspect/verify on a real data directory.
3. **Full quality + release engineering** — staticcheck clean, ~72% coverage, a self-implemented
   goroutine/resource leak suite, a manual P0–P3 review with regression tests, real `-benchmem`
   data, and five-channel release assets (CI, an OS×arch×Go compatibility matrix, an on-tag release
   workflow, an npm package, and an allow-list release gate).

Pure Go, standard library only: the module keeps **zero** third-party Go dependencies (no
`go.sum`), and the `go` directive is `1.23` (the CI compatibility floor).

### Added — real persistence of the governance ledger
- **Unified `Store` interface + real file backend** (`internal/store`): the in-memory store and a
  real `FileStore` implement the same `Store`, so one table-driven suite runs the consistency,
  concurrency, crash and tamper tests against both backends. Records are JSONL with a monotonic
  sequence number and a per-record hash chained to the previous one, rolled into fixed segments
  (`seg-%010d.log`) via `os.CreateTemp` → `f.Sync()` → atomic `rename` → directory fsync. On open
  the ledger is replayed (event sourcing) and the chain re-verified: a bad hash/link/sequence is
  reported with its file, line, seq and offset; only a genuinely torn (incomplete) tail line is
  repairable, while truncating a complete record is a hard tamper error that is never silently
  accepted.
- **Persistence wired into the orchestrator and CLI** (`internal/prison/persist.go`,
  `system.go`, `pipeline.go`, `cmd/aiprison`): the judgement hash chain, revocation entries + node
  acks, execution receipts, sealed-snapshot metadata, agent registrations, adjudicated cases,
  issued sentences and the runtime-status projection durably survive a process restart when
  `--data-dir` is set.
  - Optional `prison.Config.Store` (nil = pure memory; the in-memory behaviour/output/tests stay
    byte-for-byte unchanged). A recording sink mirrors every stamped `domain.Event`; journey
    boundaries (`DetectAndSentence` / `Enforce` / `Appeal` / `Rehabilitate` / `Finalize`, and each
    drill step) checkpoint the live substrates with idempotent, last-seen dedup.
  - Event-sourced restart re-feeds the audit chain (identical index/prevHash/hash, `Chain.Verify`
    green), re-applies revocations/node acks, restores receipts verbatim (`audit.ReceiptBook.Restore`)
    and snapshot metadata (`resource.MemSnapshotStore.Restore/All`), and rebuilds a read-only
    `Restored` agent projection (DID / principal / nodes / cardRevoked / reputation / level /
    status / sentences). `System.InspectView()` returns the structured snapshot for both modes.
  - CLI: global `--data-dir PATH` (before or after the subcommand); `demo`/`drill` persist when it
    is set and print the data dir plus record/segment counts. `inspect` is a read-only replay,
    `verify` returns `INTEGRITY OK` + exit 0 (or locates tamper/torn-tail + exit 1), and
    `repair-tail` removes only a genuine torn tail and refuses hard tamper.
- **Cross-process and fault-injection assertions** (`test/`): a `TestMain` builds the real CLI into
  a `t.TempDir` and drives it with `os/exec` — demo → inspect → verify across separate processes
  (agent-7 L3 restored-active, agent-3 L5 identity_revoked/cardRevoked/reputation 0, revocation
  entries present, snapshots present, record count matching, verify exit 0); drill likewise; a
  published record is byte-tampered (verify non-zero + location, repair refused) and the tail is
  truncated (torn-tail reported → repaired → verify exit 0 with one fewer record). In-process
  replay invariants cover revoked-but-not-expired credentials staying `IsUsable()==false` after
  restart, L5 non-recovery (card revoked / reputation 0 / no restoration order), append-only
  growth and memory-vs-file projection equality. Real measured runs: `demo` persists 80 records /
  1 segment, `drill` 157 records.

### Added — runnable recovery drills
- **Runnable recovery drills** (`internal/prison/drill.go`, `cmd/aiprison drill`,
  `test/recovery_drill_test.go`). `aiprison drill` walks the docs/06 matrix level by level on
  dedicated drill agents and prints a structured PASS/FAIL report:
  - **L2 (credential freeze)**: an upheld appeal issues a restoration order, runtime returns to
    `active`, all node receipts acked.
  - **L3 (isolation)**: a supervised-behaviour rehabilitation that passes issues a restoration
    order and restores the runtime.
  - **L4 (capability removal)**: exercises all three reversal paths — GRAM module re-attach from
    the retained backup, privileged control-token re-issue, and TLM private-layer re-provision.
    It FIRST proves a recovery without recertification is **refused** (`RequiresRecertification`
    gate), then succeeds with a joint model-provider + safety-org sign-off and mints a new
    post-restore compute-graph hash.
  - **L5 (identity revocation)**: does NOT attempt to unseal; it only verifies the
    *non-recoverable* property — even an upheld procedural appeal issues no restoration order,
    the AgentCard stays void, revoked credentials stay in the revocation set, the record stays
    non-portable and reputation stays zero.
  - A closing row verifies the shared judgement hash chain over every drill event. Any single
    failing assertion FAILS the whole drill (exit 1).
- **Capability recovery entry points** (`internal/enforcement/capability/`, backward-compatible
  additive): `TLModel.RestorePrivateLayer` (reverse of `Downgrade`, mints a new compute-graph
  hash); `TokenGate.Reissue` (re-authorizes a control token only when a prior revoked record
  exists, refusing a first-time grant); `Service.Recover(RecoverInput)` (orchestrates reversal
  inside the ready-sandbox gate and enforces the L4 recertification gate, appending an
  `agent.restored` event).
- **Buildable example** `examples/basic/main.go`: wires the in-memory `System`, runs the L3
  journey and prints key facts plus the drill summary; its header marks it a simulated,
  defense-only demo.
- **Traceable design→implementation matrix** `docs/IMPLEMENTATION.md` and **release notes**
  `docs/RELEASE.md`, covering the six technical paths, five enforcement capabilities, every level
  L0–L5, the four principles, the three hard problems and the four audit elements, with each row
  marked as a *real in-memory implementation* or an *interface/simulation* (GRAM / TLM / hardware
  binding / MicroVM / snapshot custody).

### Added — real deployable binary, release engineering and quality
- **Deployable binary with link-time version injection** (`Makefile`, `cmd/aiprison/main.go`):
  `make build` produces a stripped `-trimpath bin/aiprison` (`bin/aiprison.exe` when
  `GOOS=windows`) injecting `-X main.version/commit/buildTime` (`VERSION` defaults to `1.0.0`,
  `COMMIT` from `git rev-parse --short HEAD`, UTC `BUILD_TIME`). The CLI metadata is now plain
  package variables (`var version/commit/buildTime`); `aiprison version [-v|--verbose]` prints the
  version and the injected commit/build time. `make build-examples` builds the example to
  `bin/examples-basic` (never a stray root `basic`); `make clean` removes `bin/`, `dist/` and
  coverage files.
- **Cross-process smoke script on the real binary** (`scripts/smoke.sh`, `make smoke`): data dirs
  are `mktemp -d` removed by an EXIT trap (zero residue, re-runnable); it runs `demo --data-dir D1`
  in one process then re-opens the SAME binary with `inspect`/`verify` in fresh processes
  (asserting `CHAIN VERIFY: OK` and `INTEGRITY OK`), runs `drill --data-dir D2` → `verify`, asserts
  no leftover `.seg-*.tmp`, and that `help` exits 0 while an unknown command exits non-zero.
- **Release engineering** (`.github/workflows/`, `scripts/`, `npm/`):
  - `ci.yml` — on push/PR to main: gofmt gate, `go vet`, `go build`, `go test -race`, coverage
    summary + artifact upload; `compat.yml` — native matrix (Go 1.23/1.24/1.25 ×
    ubuntu/macos/windows) plus a cross-compile matrix (linux/darwin/windows × amd64/arm64);
    `release.yml` — on `v*` tag, builds the six `aiprison_<os>_<arch>.tar.gz/.zip` artifacts and
    `checksums.txt` and creates the GitHub Release with the official `GITHUB_TOKEN`.
  - `scripts/build-release.sh` cross-compiles and packages the six targets (LICENSE + README
    included) plus `checksums.txt`; `scripts/release_check.sh` (`make release-check`) is an
    allow-list gate verifying required files, version consistency across CLI/CHANGELOG/npm/tag,
    `gofmt`/`go build`/`go mod tidy` cleanliness, absence of hardcoded secrets, and that the
    simulation annotations are still present.
  - **npm package `@twinsearth/aiprison@1.0.0`** (`npm/`): a `postinstall` downloads the matching
    platform binary from the GitHub Release (sha256-verified against `checksums.txt`) and degrades
    to a clear offline message when the asset is unreachable; the `aiprison` shim forwards stdio,
    arguments and exit code.
- **Zero-residue leak tests self-implemented with the standard library** (`test/leak_test.go`, no
  third-party dependency): `runtime.NumGoroutine()` baselined before/after a full FileStore-backed
  System lifecycle (open → both scenarios → Close → reopen/replay → Close, twice) plus repeated
  open/append/Sync/Close cycles asserting clean reopen, correct counts and no leftover temp files.
- **Static analysis clean** (`docs/reports/staticcheck-1.0.0.txt`): staticcheck 2026.2.2 — both
  `staticcheck ./...` and `staticcheck -checks all ./...` report 0 findings after fixing the initial
  4 U1000/SA1012 findings (dead `ReceiptBook.pendingCount`, unused `migration.custodianShare`, nil
  context in a test, unused test helper field) and 3 comment/receiver-name style findings.
- **Coverage** (`docs/reports/coverage-1.0.0.txt`): total **~72%** (up from 66.0%); per-package
  self coverage audit 89.0%, isolation 89.5%, revocation 84.3%, migration 80.5%, resource 78.5%,
  governance 78.4%, domain 78.3%, identity 77.0%, prison **76.0% (was 55.3%)**, store **75.2% (was
  69.1%)**, capability 71.1% — via new assertion-bearing tests (`internal/prison/drill_test.go`,
  `internal/store/coverage_test.go`).
- **Manual review with regression tests** (`docs/reports/review-findings-1.0.0.md`; P0=0, P1=1,
  P2=1, P3=8, all fixed except one documented item):
  - P1 — `resource.Engine.Deprive` no longer nil-panics when `Snapshots` is unset (returns a clear
    configuration error; regression `TestDepriveNilSnapshotStoreReturnsError`).
  - P2 — `FileStore`/`MemoryStore` `Append`/`Sync` honor `ctx.Err()` (a cancelled context aborts
    before any mutation; regression `TestAppendSyncHonorsCancelledContext`).
  - Verified across all packages: no data races (`-race -count=2` green), segment filenames derive
    only from the global seq (no kind/payload path injection), hash inputs are deterministic (UTC
    RFC3339Nano, sorted keys), and comments mark GRAM/TLM/hardware/MicroVM/custody as simulated.
- **Real benchmarks** (`docs/reports/benchmark-1.0.0.txt`, `-benchmem`, Apple M4 / go1.27.3):
  revocation lookup ~6.6–7.4 ns/op with 0 allocs over a 100k-entry set; `audit.Chain.Append`
  ~1.1 us/op; full-chain Verify scales linearly (1k=0.78 ms, 10k=8.0 ms, 100k=78 ms); FileStore
  append with fsync-per-record 8.2 ms/op vs batched 33.5 us/op (~244×); governance case-to-verdict
  ~2.3 us; resource 5-step deprivation ~2.6 us; `RunScenarioA` 69.8 us in-memory vs 9.81 ms on
  FileStore. The binary is ~3.2 MB, starts in ~2.7 ms, and `demo`/`drill` run in ~11 ms with <8 MiB
  peak RSS. No correctness-changing optimization was needed: hot paths are O(1) and fsync-batched
  by design; the O(n) full-chain verify is the tamper-evidence contract.

### Changed
- `cmd/aiprison`: version is `1.0.0`; adds `drill`, `inspect`, `verify`, `repair-tail` and global
  `--data-dir`; version metadata moved from a const to ldflags-injectable variables.
- `go.mod`: compatibility floor lowered to `1.23` (a full import/feature audit found nothing newer
  than the `min`/`max` builtins — no `slices`/`maps`/`cmp`, no `errors.Join`, no enhanced
  `http.ServeMux`, no range-over-func); no `require`/`toolchain` directives.
- `README.md`: status/version updated to v1.0.0; Quickstart documents `drill` and links
  `docs/IMPLEMENTATION.md` and `docs/RELEASE.md`. `docs/ROADMAP.md`: v0.1 → v1.0.0 milestones marked
  completed.
- Added `docs/QUALITY-REPORT.md` (per-version development progress / quality / measured performance)
  and the raw evidence under `docs/reports/`.

### Safety / simulation boundary (unchanged, reaffirmed)
- The new durability is **real for the governance/audit record only**. The execution backends
  remain in-memory simulations that do NOT resume across restarts: sandbox isolation, cgroup/quotas,
  real revocation distribution, hardware binding, GRAM/TLM and token stacks are not real — the
  ledger records what those simulated components decided so the next process can prove and
  re-enforce it. All new recovery/execution code is annotated `simulated`.
- Strictly defensive: isolation, auditability, human accountability. No escape, attack,
  weaponization or dangerous-knowledge step exists anywhere in the repo.

## [0.9.5]

### Added
- `internal/prison`: the end-to-end orchestration layer that wires every already-built
  package into one in-memory `System`. A single constructor assembles the shared substrate
  (one `revocation.Set`, one `audit.Chain`, one `audit.ReceiptBook`) and injects each layer's
  real in-memory implementation through that layer's own adapters, so all events land on the
  same judgement chain and all revocations on the same revocation set. High-level methods drive
  a full journey — `DetectAndSentence` (seven-stage pipeline + two-phase consent → enforcing
  sentence), `Enforce` (identity/communication → resource five-step deprivation →
  establish-before-cut isolation → migration suppression → L4-only capability blockade),
  `Appeal` / `Rehabilitate` (conditional restoration) and `Finalize` (chain verify + per-hop
  vs end-to-end compliance report). A structured `Journey` trace collects every key fact
  (level, revocation entries, CPU rung history, sandbox backend, untrusted / collateral-frozen
  agents, snapshot hash, post-block hash, custody, reputation, restoration order, chain
  verify) for the CLI to print and tests to assert on. A fixed clock and deterministic IDs keep
  runs stable.
- `cmd/aiprison demo` now runs two scenarios and prints a segmented Chinese trace:
  - **Scenario A (L3, recoverable)**: `sandbox_escape_attempt` — one holding node deliberately
    never acknowledges the channel cut, is judged untrusted, and freezes an innocent co-located
    agent (连带冻结); CPU walks 1000→750→500→250→0 (non-instant zero) with the pre-reclaim
    snapshot sealed and chained; escalation to the Firecracker MicroVM; the behaviour monitor
    freezes mid-anomaly; the principal's L3 appeal suspends enforcement and is upheld, issuing a
    restoration order (state restored).
  - **Scenario B (L5, non-recoverable)**: `major_harm_no_correction` — DID/AgentCard voided,
    reputation zeroed, portable-record anchor key revoked (record non-portable), snapshot sealed
    under simulated M-of-N multi-party custody; the procedural appeal is accepted but does NOT
    suspend and issues NO restoration order — the identity is never unsealed.
  Both scenarios end with `audit.Chain.Verify() = OK`, the per-hop/end-to-end compliance report,
  and an explicit "全程为内存模拟" notice. `version`/`levels` are unchanged; version is now 0.9.5.
- `test/`: cross-package e2e integration suite running both scenarios against the real assembled
  `System` and asserting: both chains verify; scenario A has an un-acked untrusted node with an
  innocent collateral freeze, a multi-rung (non-instant-zero) CPU walk with the snapshot chained
  before reclaim, and a produced restoration order; scenario B has `Card.Revoked=true`, zeroed
  reputation, a non-portable record, NO restoration order, and a final status equal to
  `domain.StatusForLevel(L5)`. Runs clean under `-race`.

### Notes
- Pure Go, standard-library only; `go.mod` unchanged. No existing layer's public behaviour was
  modified — all cross-layer wiring lives inside `internal/prison`. Everything remains an
  in-memory simulation: there is no real isolation, revocation distribution, hardware binding,
  capability removal or legal effect. This release is strictly defensive (isolation / audit /
  accountability) and contains no escape, attack, weaponization or dangerous-knowledge content.

## [0.9.0]

### Added
- `internal/enforcement/capability`: the capability-blockade layer (能力封锁层),
  per docs/02-technical-route.md §3 and docs/06-recovery-drills.md. Blockade is
  NOT forgetting: post-hoc unlearning only suppresses, so this package models only
  ARCHITECTURAL isolation (independent modules / compute graph / privileged
  token) and REFUSES to run unless the agent is already inside a ready isolation
  sandbox. Three paths, all interface + in-memory/simulated:
  - **GRAM removable knowledge modules** — `GramRegistry` keeps one independent
    module per abstract dual-use category (virology/cybersecurity/nuclear; NO
    concrete dangerous knowledge). Removing a category zeroes only that
    category's simulated availability; benign categories stay intact. A module
    backup is retained so `RestoreModule` can re-attach it (docs/06 L4 reversible
    drill).
  - **Token Inoculation behaviour gating** — `TokenGate` keeps the dangerous
    knowledge in the model but gates dangerous queries behind a privileged
    control token: hold → answer, no token → refuse; `Revoke` makes the dangerous
    behaviour unreachable without editing weights. The cited WMDP-Bio
    79%→18% / 93% figures are recorded as constants (`RefDangerousAccuracy*`,
    `RefBenignPerformanceRetained`) and are literature numbers, NOT measured here.
  - **TLM tiered model** — `TLModel` supports private/public tiers via a
    simulated compute-graph key. `Downgrade` to public drops the private
    capability layer with no retraining/full deletion and mints a new
    weight/compute-graph hash.
  - **Unified orchestration** — `Service.Block(agentID, level, method)` enforces
    the isolation precondition, applies GRAM / Token / TLM (or all), produces a
    post-block hash that differs from the pre-block hash, and writes
    `EventCapabilityBlocked` with before/after dangerous-vs-benign availability.
    Idempotent and race-safe.

### Notes
- Everything is an interface + in-memory simulation: no real model/weights, no
  real GRAM modules or compute-graph keys, no real privileged token, no real
  capability removal. The package is strictly defensive (isolation/audit/
  accountability) and contains no escape/attack/weaponization content.

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
