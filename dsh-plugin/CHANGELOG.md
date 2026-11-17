# Changelog

All notable changes to `@twinsearth/ai-prison-dsh-plugin` are recorded here.

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
