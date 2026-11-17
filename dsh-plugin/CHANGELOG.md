# Changelog

All notable changes to `@twinsearth/ai-prison-dsh-plugin` are recorded here.

## [1.1.4] — 2026-11-18

### Fixed

- **Run drill failed on a second drill**: like `demo`, the CLI replays an
  existing ledger and then refuses to re-seed credentials that an earlier drill
  already revoked, so pressing **Run drill** on a ledger that already contained
  drill events failed with `seed allocate: … credential "cred:resource:drl-N" is
  present in the global revocation set`. A fresh ledger passes (157 records), and
  demo-then-drill passes (233 records); only a second drill failed.

### Changed

- **Both actions now start from a clean baseline**: pressing **Run demo** or
  **Run drill** clears the CLI's own `seg-*.log` files first (two-click
  confirmation when the ledger is not empty), so every run is deterministic.
  The action posts `reset-demo` / `reset-drill` to `POST /api/ai-prison/run`;
  the separate rebuild button is gone.
- `ai_prison_drill` attaches a `hint` for replay/seed-allocation failures, and
  the panel explains the clear-first behaviour under the buttons.

## [1.1.3] — 2026-11-18

### Added

- **Ledger rebuild action**: the panel now has **Rebuild ledger (clear and run
  demo)** with a two-click confirmation, backed by `action: "reset-demo"` on
  `POST /api/ai-prison/run`. It deletes only the CLI's own `seg-*.log` files in
  the ledger directory and then runs the demo, so a used ledger can be rebuilt
  without shell access. The stale `segments` count is shown too.
- `ai_prison_demo` now attaches a `hint` when the CLI refuses because the ledger
  is not empty.

### Fixed

- **Run demo on a non-empty ledger**: the CLI replays an existing ledger and then
  refuses to re-seed credentials that are already in the revocation set, so
  pressing **Run demo** after any records existed failed with
  `seed allocate: … credential … is present in the global revocation set`.
  The panel now disables that button when the ledger has segments, explains why,
  and offers the rebuild action instead; the Host route also pre-checks and
  answers `ledger-not-empty` with the same guidance.

## [1.1.2] — 2026-11-18

### Fixed

- **Tool ledger default**: `workingDirectory.get` expects a `Session`, but the
  tool execution context only carries an Agent id, so the lookup threw and every
  `ai_prison_*` tool fell back to the Host launch directory
  (`…/.dsh/profiles/desktop/.ai-prison`). The tools now resolve the Session
  through `ctx.sessions.get(agent.id)`, then its effective directory, then its
  original header `cwd`, and only then the Host default. Ledgers therefore land
  in the session workspace by default, matching the panel.

## [1.1.1] — 2026-11-18

### Added

- **Panel actions**: the audit-chain card now has **Run demo (create ledger)** and
  **Run recovery drill** buttons plus a last-action line. They call the new
  `POST /api/ai-prison/run` Host route (authenticated through
  `ctx.connection.fetch`), which runs the bundled CLI with the resolved ledger
  directory; the panel itself still never writes on its own.

### Fixed

- **Panel ledger directory**: the status route resolved `process.cwd()` (the
  profile directory, e.g. `…/.dsh/profiles/desktop/.ai-prison`) because a Fetch
  route carries no Agent. It now resolves the most recent live Session's
  effective directory through `ctx.sessions` + `ctx.workingDirectory`, so the
  panel and its action buttons use the session workspace by default.

## [1.1.0] — 2026-11-17

First release: the AI Prison governance prototype as a DeepSeek Harness bundle.

### Added

- **Seven model-facing tools**: `ai_prison_levels`, `ai_prison_status`,
  `ai_prison_demo`, `ai_prison_drill`, `ai_prison_inspect`, `ai_prison_verify`,
  `ai_prison_repair_tail`. Each call runs the bundled `aiprison` CLI and returns
  `{ ok, command, dataDir, exitCode, timedOut, stdout, stderr }` with bounded
  output, so a failing call reports instead of ending the turn.
- **Sidebar panel** (id `ai-prison`, order 2, shield icon): the L0–L5 grading
  matrix, the ledger directory, integrity pill (`INTEGRITY OK` / `Check failed` /
  `Not initialized`), record and file counts, CLI version/source, refresh button
  and a 20-second auto refresh. Read-only.
- **Read-only status API** `GET /api/ai-prison/status` registered through
  `ctx.connection.fetch`, the same authenticated channel the Web UI uses.
- **Bundled CLI**: six cross-compiled `aiprison` builds
  (`windows`/`macos`/`linux` × `amd64`/`arm64`), plus `cliPath` /
  `AIPRISON_BIN` / `PATH` fallbacks.
- **Bilingual display metadata** (`locale/en.json`, `locale/zh.json`) with a
  single-image detail document, original icon and preview illustration.
- **Release machinery**: `scripts/build-binaries.mjs`,
  `scripts/release-check.mjs`, npm tarball checks, market index
  (`market/index.json`), and a `dsh-plugin-v*` GitHub Actions release workflow.

### Known limits

- GRAM, TLM, hardware binding, MicroVM isolation and revocation distribution are
  simulated; the tools demonstrate and audit governance, they do not enforce
  real capability removal.
- Ledger writes remain limited to `demo`, `drill` and `repair-tail`.
