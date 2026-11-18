# Changelog

All notable changes to `@twinsearth/ai-prison-dsh-plugin` are recorded here.

## [1.4.7] — 2026-11-18

### Added — panel/remote operations roll back while paused or frozen

- The 5 GB freeze (and the 3.9 GB pause without a confirmation) now also covers
  operations started from the panel or the web UI, not just the agent tool:
  after an additive operation settles, the watchdog immediately disables that
  bundle and records a `freeze-rollback` / `pause-rollback` incident. Release
  operations (disable/remove) and a confirmed pause grace window stay allowed.
- The panel's "Measure plugin budgets now" button is disabled while frozen,
  because each probe spawns a child process.

### Fixed — tamper monitor no longer flags the manager's own writes

- The operation-window suppression was ±5 s around settle, but pnpm may write
  package.json / pnpm-lock.yaml later than that, so installs of 1.4.5/1.4.6 were
  repeatedly "restored" by the tamper monitor and some installs failed.
- Any drift within 120 s after the last plugin operation is now treated as a
  manager write; the mtime window still applies to older operations. Real
  out-of-window edits (manual scripts) are still detected and restored.

### Tests
- New `test/watchdog-freeze.test.mjs`: freeze rollback, pause rollback with and
  without grace, and release-class operations never rolled back.

## [1.4.6] — 2026-11-18
## [1.4.6] — 2026-11-18

### Added — structured reason / impact / steps / release text

- New `lib/remediation.js` explains every enforcement action in four fixed
  sections: why it happened, what it affects, what to do (numbered steps) and how
  the plugin leaves the prison.
- Freeze, pause and every quarantine reason (hot-ops, crash, budget, tamper) now
  carry a `remediation` object in the incident, the quarantine entry and the
  crash/tamper plan.
- The panel "Plugin prison" renders it under each jailed plugin (title, why,
  impact, numbered steps, release path); the agent tool returns the same object.
- Tests cover every remediation reason and the quarantine payload.

### Notes
- This completes the three follow-ups requested for the probe work: the probe
  publish fix and the full background probe queue shipped in 1.4.3/1.4.4; this
  release adds the operator-facing explanation.

## [1.4.5] — 2026-11-18
## [1.4.5] — 2026-11-18

### Fixed — tamper monitor false positives (important)

- The first tamper monitor attributed writes at **poll time**, not at write time, so
  a legitimate plugin-manager write (install/enable rewrites package.json and
  pnpm-lock.yaml) was classified as tampering 30 s later. That closed and jailed
  plugins after every install.
- Operation windows are now recorded on `started`/`settled` and matched against
  each changed file's **mtime** (±5 s), so a manager write stays legitimate even
  when the poll happens after the window closed.
- One-time migration: tamper quarantine persisted by 1.4.3/1.4.4 is cleared with
  an audited `migration` incident instead of re-closing those plugins on boot.

### Fixed — budget probe could not import plugins that use harness peers

- `budget-child.mjs` now resolves `@deepseek-ai/*` and `cordis` from the DSH
  installation via `module.registerHooks`, so plugins like `dsh-context` that
  import `@deepseek-ai/dsh-session` at module scope can be measured.
- Also fixed a wrong `pathToFileURL` import that broke the probe child entirely.

### Tests
- mtime operation-window suppression, legacy tamper-quarantine migration, and the
  probe child import path are covered by the test suite (70 tests total).

## [1.4.4] — 2026-11-18
## [1.4.4] — 2026-11-18

### Fixed — per-plugin budget probe actually works end to end

- The probe now covers **every enabled, non-exempt bundle without a current
  measurement** (not only plugins installed/enabled in the last 30 minutes),
  scheduled after boot (max 5 per run) and retried after failures (error entries
  older than 10 minutes, up to 5 attempts).
- Budgets are version-aware: an entry measured for another version, without a
  version, older than 7 days, or failed is treated as stale and re-measured.
- A manual "Measure plugin budgets now" button in the Enforcement card and a
  `probe` action on the watchdog route run the queue immediately (up to 15
  targets) and report `probeProgress`.
- The measured module-load MB **wins over operation estimates in Top5** and is
  labelled `source: "measured"`; operation-derived rows stay `estimate`.
- >500 MB still quarantines, closes and queues the plugin in
  `pending-boot.json`; 300–500 MB records a warning only.
- Tests: `test/watchdog-budget.test.mjs` covers the 600 MB quarantine path,
  the 420 MB warning path with measured Top5, and failed-probe retry counting.

## [1.4.3] — 2026-11-18
## [1.4.3] — 2026-11-18

### Added — profile tamper monitor (requirement 3, the missing half)

- **Hash watch of the critical profile files** (`package.json`,
  `cordis.patch.yml`, `cordis.yml`, `pnpm-workspace.yaml`, `pnpm-lock.yaml`):
  a baseline with the original bytes is captured at watchdog start, the files are
  re-hashed every 30 s, and the first 15 s after startup are treated as normal
  startup writes.
- A drift **outside a plugin-manager operation window** (±4 s around
  started/settled) is tampering: the changed bytes are copied to
  `.ai-prison-watchdog/tampered/<ts>/`, the original bytes are restored by
  default (`tamperPolicy: 'alert'` keeps the change), and an L1 plan is
  attributed from the operation journal (in-flight install/enable first). In
  enforce mode the attributed plugin is quarantined, closed and queued in
  `pending-boot.json` for the external guard.
- New `tamper` block in the watchdog snapshot/tool output, and a "Profile
  watch" line in the panel Enforcement card.

### Fixed
- `lib/budget-child.mjs` was missing from the npm package (`files` only
  matched `lib/*.js`), which made every per-plugin budget probe fail with
  "Cannot find module". `files` now ships `lib/*.mjs` too.

### Tests
- New `test/tamper-monitor.test.mjs`: snapshot/diff/restore, real runtime drift
  detection with restore, operation-window suppression, and the alert policy.

## [1.4.2] — 2026-11-18
## [1.4.2] — 2026-11-18

### Added
- Panel scrolling: the AI Prison page is a bounded scroll container now
  (`height/maxHeight: 100%`, `overflow-y: auto`, thin scrollbar gutter,
  `overscroll-behavior: contain`), so the page shows a vertical scrollbar and
  the mouse wheel scrolls the card list instead of clipping the content.

### Fixed
- CI: `test/harness.test.mjs` asserted Windows separators and failed on the
  ubuntu runner. The test now builds expectations with `path.join`, so the
  release workflow passes on every platform.

## [1.4.1] — 2026-11-18
## [1.4.1] — 2026-11-18

### Fixed — tools vanished after switching the host to the real-Node runtime

- `lib/harness.js` resolved `@deepseek-ai/dsh-tools` only from paths next to
  `process.execPath`. When the desktop host runs on the bundled Node with the
  runtime extracted to `resources/dsh-runtime/dsh`, `execPath` no longer sits
  next to the app, so `defineTool` was unavailable and `ai_prison_*` tools were
  never registered (the watchdog row kept running).
- `installationAnchors` now also checks `DSH_INSTALL_DIR` and the host's second
  argument (the runtime root) before the legacy desktop paths.
- New `test/harness.test.mjs` covers extracted-runtime, argument fallback and
  Electron-as-Node layouts.

## [1.4.0] — 2026-11-18
## [1.4.0] — 2026-11-18

### Added — P2 crash enforcement + per-plugin budgets, P3 external guard

- **Crash enforcement (requirement 3/4)**: `crashFallback` defaults to `enforce`
  now. A crash attributed to a plugin quarantines it, disables it live, and with
  L2/L3 also closes the bundles that exceed the clean baseline (L2) or everything
  outside the baseline (L3, never below base + web-app). Plans persist in
  `state.json`, a machine-readable `pending-boot.json` is written for the
  external guard, and quarantined targets are re-disabled on the next boot.
- **Per-plugin startup budget (requirement 1)**: a new isolated probe
  (`lib/budget-probe.js` + `lib/budget-child.mjs`) spawns the bundled Node with
  `--expose-gc`, imports one plugin package from the profile and reports its
  retained module-load delta. Plugins installed or enabled in the last 30 minutes
  are probed automatically after boot (max 5, only at the `ok` waterline);
  results land in `budgets` inside `state.json`. Above 500 MB the plugin is
  quarantined and closed (pending the next boot for a hard block); above 300 MB
  an incident is recorded.
- **External guard (P3)**: `E:ToFuture	oolsaiprison-guard.ps1/.cmd` runs before
  DSH, reads `pending-boot.json`, backs up the profile `package.json`, removes
  the bundles that must stay closed (or keeps only the L3 baseline), and then
  launches DSH. It is the only component that can break an import-phase crash
  loop, where the in-process watchdog never runs.

### Fixed
- Watchdog probes keep the event loop alive no longer (timer `unref`), keeping
  the test suite fast and the host responsive.

## [1.3.0] — 2026-11-18
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
