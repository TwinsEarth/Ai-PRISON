# Roadmap

This roadmap turns the source design into a buildable, per-version plan.
Each version is an independently compilable, tested and locally tagged
milestone. The versioning follows [semver](https://semver.org/); pre-1.0
versions may still reshape internal APIs.

## Per-version release gate (every version, no exceptions)

```bash
gofmt -w .
go vet ./...
go build ./...
go test -race ./...
```

- All four must be green.
- The cardinal rules need targeted unit tests:
  - revocation is checked **before** expiry;
  - unacknowledged nodes are frozen as a group (连带冻结);
  - resource reclamation is **gradual**, never an instant zero;
  - L0–L5 escalation / recovery conditions;
  - the hash chain is tamper-evident.
- Docs, JSON Schemas and Go types stay in agreement.
- Real (in-memory/simulated) implementations are clearly distinguished from
  interface placeholders for frontier capabilities (GRAM / TLM / hardware
  binding / MicroVM).
- `CHANGELOG.md` is updated and a local git tag is created.

## Milestones

### v0.1 — Foundation & contracts
- Repo init, MIT license, Makefile, `.gitignore`, community files.
- `internal/domain`: Agent, DID, AgentCard, Level (L0–L5), Violation,
  Credential, Sentence, Event, Receipt, Accountability, Reputation.
- L0–L5 rules: escalation, recovery, permanence, appeal suspension,
  reputation effects.
- `pkg/contracts` public wire contract; `schemas/` JSON Schema (2020-12).
- Full documentation set with Mermaid diagrams.
- Minimal `cmd/aiprison` (`version`, `levels`).

### v0.2 — Global revocation set
- Read-only global revocation set with append-only write semantics.
- Enforced validation order: **revocation first, then expiry**.
- Membership lookup used by `domain.Credential.IsUsable`.
- Tests for the "revoked but not expired" cache-window case.

### v0.3 — Hash chain & receipts
- Tamper-evident judgement hash chain (each event links to the prior hash).
- Signed execution receipts (operation, before/after state hash, node id).
- Per-hop vs end-to-end compliance evaluation.
- Tamper detection and cross-node verification primitives.

### v0.4 — Resource deprivation & snapshot sealing
- Revocable quota credentials (compute / memory / storage).
- Sequence: write revocation set → stop new allocation → gradual reclaim →
  forced receipts → snapshot sealing.
- Gradual downgrade (never instant zero, to avoid dirty state on crash).
- Simulated snapshot storage with hash recorded on the chain.

### v0.5 — Isolation gradient
- Sandbox backend interface: Wasmtime → Docker → Firecracker MicroVM →
  Full VM, mapped to levels and escalated step by step.
- Behaviour-monitor interface (syscall / network / file access signals) that
  freezes *during* anomalous behaviour, not after escape.
- In-memory/simulated backend; clearly labelled as simulation.

### v0.6 — Identity & communication
- DID / AgentCard revocation and voiding.
- Communication cut-off with forced receipts; unacknowledged nodes are marked
  untrusted and their agents frozen as a group.
- Human accountability coupling (principal / deployer / model provider).

### v0.7 — Governance state machine
- Detection → adjudication → sentence → appeal → rehabilitation.
- Committee multi-seat voting with **seven-stage decision + two-phase
  consent** (deprive? then grade?); dissents recorded immutably.
- Appeal: L0–L3 may suspend; L4–L5 do not suspend; overturn triggers full
  restoration.
- Rehabilitation evaluation (behaviour distribution back under threshold),
  with level upgrade on failure.

### v0.8 — Migration suppression
- Hardware fingerprint binding (simulated; detects casual copying).
- Portable work-record anchoring key revocation (reputation/history cannot
  migrate after L5).
- Multi-party encrypted snapshot custody (simulated).

### v0.9 — Capability blocking
- Three interfaces + simulations: GRAM removable knowledge modules,
  Token Inoculation behaviour-gating (privileged token revocation), TLM tier
  downgrade.
- Blocking performed inside the isolated sandbox; post-block weight/graph
  hash recorded.
- Explicitly marked as interface/simulation; *not* real capability removal.
- Architectural isolation only (not post-hoc behavioural suppression).

### v0.9.5 — CLI end-to-end demo
- One offending agent flows: detection → adjudication → graduated
  disposition → receipts → appeal → rehabilitation/restoration.
- Runnable `aiprison demo`; integration test under `test/`.

### v1.0.0 — Release hardening
- Examples, recovery drills, end-to-end compliance reports.
- Documentation/schema/code consistency pass.
- Full `make check` green; `v1.0.0` tag and release notes.

## Out of scope for the prototype

- Real sandbox/VM orchestration, real kernel isolation and real hardware
  roots of trust.
- Any offensive capability (escape, exploitation, weaponization, dangerous
  knowledge extraction).
- Production-grade distributed consensus, HSM key custody, and real
  cross-jurisdictional legal integration.
