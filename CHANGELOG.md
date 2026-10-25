# Changelog

All notable changes to AI Prison are documented in this file. The format is
loosely based on [Keep a Changelog](https://keepachangelog.com/), and the
project follows [semver](https://semver.org/).

## [Unreleased]

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
