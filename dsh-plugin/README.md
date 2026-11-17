# @twinsearth/ai-prison-dsh-plugin

AI Prison for DSH — the defensive agent-governance prototype of
[Ai-PRISON](https://github.com/TwinsEarth/Ai-PRISON), exposed as a DeepSeek
Harness bundle: six `ai_prison_*` tools plus a sidebar panel that renders the
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
panel; the six tools become visible to the Agent on the next session step.

## Tools

| Tool | Effect | What it does |
|---|---|---|
| `ai_prison_levels` | read-only | Print the L0–L5 matrix (codes, names, duration class, deprived capabilities). |
| `ai_prison_status` | read-only | CLI path/source/version, the matrix, and the ledger's integrity + record count. Same data as the panel. |
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
