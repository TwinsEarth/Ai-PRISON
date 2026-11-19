# @twinsearth/ai-prison-dsh-plugin

AI Prison for DSH — the defensive agent-governance prototype of
[Ai-PRISON](https://github.com/TwinsEarth/Ai-PRISON), exposed as a DeepSeek
Harness bundle: eight `ai_prison_*` tools plus a sidebar panel that renders the
L0–L5 grading matrix and the durable ledger's audit-chain state.

Everything here is **defensive**: detection, graduated capability deprivation,
hash-chained audit, appeal and rehabilitation. GRAM, TLM, hardware binding and
MicroVM isolation are **simulated backends**, not shipped controls.

## Install

- **Plugin Manager** → **Add plugin** → install this directory or the npm package.
- **npm**: `@twinsearth/ai-prison-dsh-plugin` (bundled `aiprison` CLI for
  Windows, macOS and Linux, amd64 and arm64).
- **CLI**: `dsh plugin install @twinsearth/ai-prison-dsh-plugin` (or pass the
  absolute package directory to the plugin manager).

After enabling the bundle, a shield entry appears in the sidebar. Open it for the
panel; the eight tools become visible to the Agent on the next session step.

## Tools

| Tool | Effect | What it does |
|---|---|---|
| `ai_prison_levels` | read-only | Print the L0–L5 matrix (codes, names, duration class, deprived capabilities). |
| `ai_prison_status` | read-only | CLI path/source/version, the matrix, and the ledger's integrity + record count. Same data as the panel. |
| `ai_prison_watchdog_status` | read-only | Watchdog snapshot: memory waterline/thresholds, hot-operation Top5, quarantined plugins, crash plans, budgets, incidents. Same data as the Enforcement cards. |
| `ai_prison_demo` | writes ledger | End-to-end governance demo: detect → seven-stage adjudication → layered enforcement → audit → appeal/rehabilitation → recovery. |
| `ai_prison_drill` | writes ledger | v1.0.0 recovery drill over the reversibility matrix with per-assertion PASS/FAIL and an overall verdict. |
| `ai_prison_inspect` | read-only | Replay the ledger: hash chain, agents, sentences, revocations, receipts, sealed snapshots. |
| `ai_prison_verify` | read-only | Verify every per-record hash link; `INTEGRITY OK` + exit 0, or locate the tamper/torn tail and exit 1. |
| `ai_prison_repair_tail` | mutates | Remove only a genuine torn tail line; refuses hard tampering. Run `ai_prison_verify` first. |

The panel shows the matrix from `ai_prison_levels` data plus the ledger
directory, integrity pill, record/segment/file counts; it refreshes every 20
seconds and on demand. Below the audit-chain card sit two explicit buttons —
**Run demo** and **Run drill** — which call `POST /api/ai-prison/run` and
therefore write the ledger only when pressed; the panel never writes on its own.
Both runs start from a clean baseline: when the ledger already has segments the
first click asks for confirmation, then the CLI's own `seg-*.log` files are
deleted and the action runs, because the CLI replays an existing ledger and
refuses to re-seed credentials that earlier runs already revoked (`demo` twice,
or a second `drill`, would otherwise fail).

## Memory watchdog and plugin enforcement

A second loader row (`ai-prison-watchdog`) samples the host heap every 5 seconds,
derives the design thresholds from the real heap limit (300/500 MB per plugin;
3.0/3.9/5.0 GB and operation gates 4/5 GB on a 6 GB host, mapped down to
2.99/3.60/3.81 and 2.73/3.41 GB on a 4 GB host), accounts plugin operations
(install/remove = 1, enable = 1, disable = 0.5), estimates each plugin's heap
delta around those operations, reads new `crash-*-host.log` files at startup and
records a 500 ms startup curve into `.ai-prison-watchdog/run-*.jsonl`.
It exposes `GET /api/ai-prison/watchdog/status`, `POST /api/ai-prison/watchdog/action`
and the read-only `ai_prison_watchdog_status` tool; the panel adds **Memory
watchdog**, **Plugin prison** and **Enforcement** cards.

**Enforcement is on by default.** Exceeding the hot-operation limit (more than
five additive operations for one plugin in a run) closes and quarantines that
plugin; at the pause/freeze waterlines additive operations are blocked and
already-started ones are rolled back; agent-side `plugin_manager` calls are
rejected too. Set `config.enforce: false` on the watchdog row for observe-only
mode, which records everything but never touches a plugin.

Things that are **off by default** because they spawn processes or rewrite
files: child-process RSS scanning (`rssScan: true`), automatic budget probes
(`probeAuto: true`), tray notifications (`notify: true`); profile tamper policy
defaults to `observe` (`tamperPolicy: restore` opts into rewriting files).

Enforcement plans are also written to `.ai-prison-watchdog/pending-boot.json`
for a launcher/guard to apply **before the next start**. No such external
consumer ships with the plugin, so that file alone does not disable anything:
the in-process close/quarantine is what takes effect in the current run.
State lives in `<profile>/.ai-prison-watchdog/`.

## Ledger directory

Default for tools: `<session working directory>/.ai-prison`, so the audit trail
stays next to the project it governs. The panel resolves the most recent live
Session's effective directory for the same reason (a Fetch route carries no
Agent of its own). Override with a tool argument (`data_dir`) or with the plugin
config:

```yaml
- id: ai-prison
  config:
    cliPath: ''        # absolute path to a specific aiprison binary
    dataDir: ''        # durable ledger directory
    timeoutMs: 120000  # per-call CLI timeout
```

Resolution order for the CLI: `config.cliPath` → `AIPRISON_BIN` → the bundled
build for this platform → `aiprison` on `PATH`.

The panel's HTTP routes are deliberately narrower than the tools: a `data_dir`
sent to `GET /api/ai-prison/status` or `POST /api/ai-prison/run` must resolve to
the configured ledger directory or `<working directory>/.ai-prison`; anything
else is rejected with `403 data-dir-not-allowed` before the CLI runs. The
agent-facing tools keep the explicit `data_dir` argument.

## Bundled CLIs

`bin/<platform>-<arch>/aiprison` ships six cross-compiled builds: `windows`,
`macos`, `linux` × `amd64`, `arm64`. They are built from the Go sources in this
repository with `node dsh-plugin/scripts/build-binaries.mjs` (pure standard
library, `CGO_ENABLED=0`, no module downloads) and are not committed to git.

On a platform without a bundled build, point `cliPath` / `AIPRISON_BIN` at a
binary built from `cmd/aiprison`.

## Development

```bash
node dsh-plugin/scripts/build-binaries.mjs   # from the repository root
cd dsh-plugin
node --test test/*.test.mjs                 # unit + integration tests
node scripts/release-check.mjs              # version, files, binaries, secrets
node scripts/update-market-index.mjs        # regenerate market/index.json
```

A release is a `dsh-plugin-v*` tag: CI rebuilds the binaries, runs the tests and
the release check, packs the npm tarball and publishes the GitHub Release assets.

## Limitations

- Backends are simulations: no real isolation, revocation distribution,
  hardware binding or capability removal happens.
- The watchdog is an in-process observer: it cannot sample or act while the host
  event loop is blocked, before its loader row is initialized, or after the host
  exits; it does not replace a process-external memory guard.
- The plugin never writes the ledger from status calls; only the two explicit
  panel buttons and the `demo`, `drill` and `repair-tail` tools mutate it.
- A wrong `data_dir` reveals that directory's integrity state only; the plugin
  reads and checks, it does not repair anything except the torn tail the CLI
  itself accepts.
- On Windows, the aiprison binaries are unsigned; an enabled Controlled Folder
  Access policy can deny their writes outside allowed locations. Keep the ledger
  inside the working directory (the default) or allow the binary in Windows
  Security.

## License

MIT — same as the AI Prison prototype in this repository.
