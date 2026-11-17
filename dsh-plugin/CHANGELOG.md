# Changelog

All notable changes to `@twinsearth/ai-prison-dsh-plugin` are recorded here.

## [1.3.0] — 2026-11-18

### Added — P1 enforcement (requirements 2, 3, 4 dry-run, 5)

- **Memory waterline enforcement**: warn 3 GB, pause 3.9 GB (needs a panel
  second confirmation, 10-minute grace), strong warning 4 GB, freeze 5 GB on a
  6 GB host. Thresholds stay cap-relative on a 4 GB host. Hysteresis keeps a
  frozen host frozen until the heap drops below the block line minus 300 MB.
- **Hot-operation jail (requirement 2)**: `plugin-manager/operation` settled
  records are counted per target (install/remove = 1, enable = 1, disable = 0.5,
  failed/cancelled ignored, base bundles and the watchdog itself exempt). More
  than five in one run auto-disables the target through
  `pluginManager.setBundleEnabled`, quarantines it for the rest of the run and
  audits every step. Quarantine is released by restart or by the panel
  "Release" action; the agent-facing `plugin_manager` tool is denied while a
  target is jailed.
- **Tool guard**: frozen/paused states deny mutating `plugin_manager` actions
  with a reason (read-only actions stay allowed; the panel keeps release
  actions for the Top5 close buttons).
- **Crash attribution (requirement 3, dry-run)**: new crash logs are parsed for
  `hostRunId`; the plugin-operation journal is matched by that run id and the
  last changed plugin is picked (in-flight install/enable first, then the latest
  settled install, then the latest settled enable). The result is recorded as an
  L1 plan and an incident; nothing is closed until the user confirms enforcement.
- **Baseline (requirement 4, dry-run)**: a clean run records its enabled bundle
  set after 60 s without crashes, waterline trouble or quarantine; escalation
  counters L1→L2→L3 persist across restarts.
- **Panel**: new "Enforcement" card shows the freeze banner, pause confirmation,
  quarantine list with release buttons, the memory Top5 with "close now" buttons
  and the latest dry-run crash plans.
- **Tool**: `ai_prison_watchdog_status` now reports enforcement state,
  quarantine, Top5, plans and escalation.

### Notes

- Automatic disable only uses the plugin manager service; no profile file is
  edited and no plugin code is loaded or unloaded by the watchdog.
- Top5 numbers are startup-budget + operation-delta estimates, labelled as such;
  no heap snapshot is taken near the limit.

## [1.2.1] — 2026-11-18
## [1.2.1] — 2026-11-18

### Fixed

- **Watchdog row crashed before its sampler started**: `readCrashLogs()` ran during
  setup and called `persist()` while `let lastFlush` was still in its temporal dead
  zone, so the row threw a `ReferenceError` after recording crash history but before
  registering the sampler, the operation listener and the status route. The value is now
  declared before the first scan, `readCrashLogs` is wrapped defensively, and the
  runtime exposes a `dispose()` used by tests.
- Watchdog state directory and crash-log directory are now injectable
  (`config.dataDir` / `config.logsDir`) so runtime tests stay hermetic.

### Tests

- New `test/watchdog-runtime.test.mjs`: heap sampling, crash-fixture parsing and
  persistence, plugin-operation counting/estimation, failed-operation and removal
  handling, all against temporary directories.

## [1.2.0] — 2026-11-18

### Added — P0 memory watchdog (observe mode)

- **New loader row `ai-prison-watchdog`** (`watchdog.js` + `lib/watchdog-*.js`):
  samples the host heap every 5 s (`process.memoryUsage` + `v8.getHeapStatistics`),
  derives the design thresholds from the actual heap limit (300/500 MB per plugin,
  3.0/3.9/5.0 GB total, 4/5 GB operation gates), classifies the waterline
  (`ok` → `warn` → `pause` → `block`) and records threshold crossings, per-plugin
  operation counts (install/remove = 1, enable = 1, disable = 0.5) and estimated
  heap deltas per plugin.
- **Crash-history scan**: on activation it reads new
  `crash-*-host.log` files from the desktop log directory, parses
  phase/OOM/exit-code and appends them to the incident journal.
- **Read-only API** `GET /api/ai-prison/watchdog/status` and a new
  `ai_prison_watchdog_status` tool.
- **Panel**: two new cards — **Memory watchdog** (heap bar, used/limit, RSS,
  warn/pause/block thresholds) and **Plugin prison** (top-5 plugins by estimated
  heap, hot-operation counts, recent incidents). Both label themselves
  "observe mode: records only, never closes plugins".
- State is persisted under `<profile>/.ai-prison-watchdog/`
  (`state.json`, `incidents.jsonl`).

### Explicitly not in this release

- No automatic closing, disabling or quarantine: enforcement (P1–P3 of
  `AI-PRISON-WATCHDOG-DESIGN.md`) is not implemented and requires explicit
  confirmation.

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
