import assert from 'node:assert/strict'
import fs from 'node:fs'
import path from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'
import { createWatchdog } from '../lib/watchdog-runtime.js'
import { resetWatchdogState } from '../lib/watchdog-state.js'
import { createTamperMonitor, diffSnapshots, restoreBaseline, snapshotCriticalFiles } from '../lib/tamper-monitor.js'

const pluginRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')

function makeTemp(prefix) {
  return fs.mkdtempSync(path.join(pluginRoot, prefix))
}

function makeCtx() {
  const state = { operationHandler: null }
  const ctx = {
    effect: (fn) => {
      const disposer = fn()
      return typeof disposer === 'function' ? disposer : () => {}
    },
    on: (name, handler) => {
      if (name === 'plugin-manager/operation') state.operationHandler = handler
      return () => {}
    },
    inject: (deps, callback) => {
      if (deps.includes('pluginManager')) callback({ pluginManager: null, effect: (fn) => fn() })
      if (deps.includes('tools')) callback({ tools: { guard: () => () => {} }, effect: (fn) => fn() })
    },
  }
  return { ctx, state }
}

function makeProfile() {
  const dir = makeTemp('.tmp-tamper-profile-')
  fs.writeFileSync(path.join(dir, 'package.json'), JSON.stringify({ name: 'p', dsh: { profile: { bundles: ['a'] } } }, null, 2))
  fs.writeFileSync(path.join(dir, 'cordis.patch.yml'), '- insert:\n    - id: a\n')
  fs.writeFileSync(path.join(dir, 'pnpm-workspace.yaml'), 'packages: []\n')
  return dir
}

test('snapshot diff and restore report modified and deleted files', () => {
  const profile = makeProfile()
  try {
    const base = snapshotCriticalFiles(profile)
    fs.writeFileSync(path.join(profile, 'package.json'), '{"name":"evil"}')
    fs.rmSync(path.join(profile, 'cordis.patch.yml'))
    const next = snapshotCriticalFiles(profile)
    const diffs = diffSnapshots(base.files, next.files)
    assert.deepEqual(diffs.map((diff) => [diff.file, diff.kind]), [
      ['package.json', 'modified'],
      ['cordis.patch.yml', 'deleted'],
    ])
    const restored = restoreBaseline(profile, base.contents, diffs)
    assert.deepEqual(restored.sort(), ['cordis.patch.yml', 'package.json'])
    assert.equal(JSON.parse(fs.readFileSync(path.join(profile, 'package.json'), 'utf8')).name, 'p')
    assert.match(fs.readFileSync(path.join(profile, 'cordis.patch.yml'), 'utf8'), /insert:/)
  } finally {
    fs.rmSync(profile, { recursive: true, force: true })
  }
})

test('watchdog detects a profile drift outside an operation window and restores it', async () => {
  resetWatchdogState()
  const profile = makeProfile()
  const dirs = { dir: makeTemp('.tmp-tamper-wd-') }
  dirs.logs = path.join(dirs.dir, 'logs')
  dirs.journal = path.join(dirs.dir, 'journal')
  dirs.state = path.join(dirs.dir, 'state')
  fs.mkdirSync(dirs.logs, { recursive: true })
  fs.mkdirSync(dirs.journal, { recursive: true })
  const { ctx } = makeCtx()
  const watchdog = createWatchdog(
    ctx,
    {
      profileDir: profile,
      dataDir: dirs.state,
      logsDir: dirs.logs,
      journalDir: dirs.journal,
      sampleMs: 60_000,
      tamperIntervalMs: 0,
      tamperGraceMs: 0,
      tamperPolicy: 'restore',
    },
    { info() {}, warn() {} },
  )
  try {
    const before = fs.readFileSync(path.join(profile, 'package.json'), 'utf8')
    fs.writeFileSync(path.join(profile, 'package.json'), '{"name":"tampered"}')
    const result = await watchdog.tamperCheck({ force: true })
    assert.ok(result.diffs.some((diff) => diff.file === 'package.json' && diff.kind === 'modified'))
    assert.ok(result.restored.includes('package.json'))
    assert.equal(fs.readFileSync(path.join(profile, 'package.json'), 'utf8'), before)
    const snapshot = watchdog.snapshot()
    assert.ok(snapshot.incidents.some((incident) => incident.kind === 'tamper'))
    assert.ok(snapshot.plans.some((plan) => plan.reason === 'tamper'))
    assert.equal(snapshot.tamper.incidents, 1)
  } finally {
    watchdog.dispose()
    fs.rmSync(profile, { recursive: true, force: true })
    fs.rmSync(dirs.dir, { recursive: true, force: true })
  }
})

test('a drift during a plugin operation window is treated as a legitimate write', async () => {
  resetWatchdogState()
  const profile = makeProfile()
  const dirs = { dir: makeTemp('.tmp-tamper-win-') }
  dirs.logs = path.join(dirs.dir, 'logs')
  dirs.journal = path.join(dirs.dir, 'journal')
  dirs.state = path.join(dirs.dir, 'state')
  fs.mkdirSync(dirs.logs, { recursive: true })
  fs.mkdirSync(dirs.journal, { recursive: true })
  const { ctx, state } = makeCtx()
  const watchdog = createWatchdog(
    ctx,
    {
      profileDir: profile,
      dataDir: dirs.state,
      logsDir: dirs.logs,
      journalDir: dirs.journal,
      sampleMs: 60_000,
      tamperIntervalMs: 0,
      tamperGraceMs: 0,
      tamperPolicy: 'restore',
    },
    { info() {}, warn() {} },
  )
  try {
    state.operationHandler({ id: 'op-1', time: new Date().toISOString(), reason: 'bundle', target: '@a/b', stage: 'enable', enabled: true, phase: 'started' })
    fs.writeFileSync(path.join(profile, 'package.json'), '{"name":"manager-write"}')
    const result = await watchdog.tamperCheck()
    assert.equal(result.skipped, 'operation-window')
    assert.equal(result.diffs.length, 0)
    assert.equal(JSON.parse(fs.readFileSync(path.join(profile, 'package.json'), 'utf8')).name, 'manager-write')
    const snapshot = watchdog.snapshot()
    assert.equal(snapshot.incidents.filter((incident) => incident.kind === 'tamper').length, 0)
  } finally {
    watchdog.dispose()
    fs.rmSync(profile, { recursive: true, force: true })
    fs.rmSync(dirs.dir, { recursive: true, force: true })
  }
})

test('tamper policy alert keeps the changed file but still records the drift', async () => {
  resetWatchdogState()
  const profile = makeProfile()
  const dirs = { dir: makeTemp('.tmp-tamper-alert-') }
  dirs.logs = path.join(dirs.dir, 'logs')
  dirs.journal = path.join(dirs.dir, 'journal')
  dirs.state = path.join(dirs.dir, 'state')
  fs.mkdirSync(dirs.logs, { recursive: true })
  fs.mkdirSync(dirs.journal, { recursive: true })
  const { ctx } = makeCtx()
  const watchdog = createWatchdog(
    ctx,
    { profileDir: profile, dataDir: dirs.state, logsDir: dirs.logs, journalDir: dirs.journal, sampleMs: 60_000, tamperIntervalMs: 0, tamperGraceMs: 0, tamperPolicy: 'alert' },
    { info() {}, warn() {} },
  )
  try {
    fs.writeFileSync(path.join(profile, 'cordis.patch.yml'), 'replaced: true\n')
    const result = await watchdog.tamperCheck({ force: true })
    assert.deepEqual(result.restored, [])
    assert.equal(fs.readFileSync(path.join(profile, 'cordis.patch.yml'), 'utf8'), 'replaced: true\n')
    assert.ok(watchdog.snapshot().incidents.some((incident) => incident.kind === 'tamper' && incident.file === undefined))
  } finally {
    watchdog.dispose()
    fs.rmSync(profile, { recursive: true, force: true })
    fs.rmSync(dirs.dir, { recursive: true, force: true })
  }
})

test('a write inside a plugin-manager window is not tampering even when polled later', () => {
  const profile = makeProfile()
  const dataDir = makeTemp('.tmp-tamper-mtime-')
  try {
    const windowEnd = Date.now() - 10_000
    const windowStart = windowEnd - 5_000
    const monitor = createTamperMonitor({
      profileDir: profile,
      dataDir,
      graceMs: 0,
      intervalMs: 0,
      isWithinOperationWindow: (mtimeMs) => mtimeMs >= windowStart - 5_000 && mtimeMs <= windowEnd + 5_000,
      onTamper: () => {
        throw new Error('operation-window write must not be reported as tampering')
      },
    })
    fs.writeFileSync(path.join(profile, 'package.json'), '{"name":"manager-write"}')
    const seconds = (windowStart + 1000) / 1000
    fs.utimesSync(path.join(profile, 'package.json'), seconds, seconds)
    const result = monitor.checkNow()
    assert.equal(result.diffs.length, 0)
    assert.equal(result.skipped, 'operation-window')
    assert.equal(JSON.parse(fs.readFileSync(path.join(profile, 'package.json'), 'utf8')).name, 'manager-write')
  } finally {
    fs.rmSync(profile, { recursive: true, force: true })
    fs.rmSync(dataDir, { recursive: true, force: true })
  }
})

test('legacy tamper quarantine from 1.4.3/1.4.4 is cleared once with an audited migration', () => {
  resetWatchdogState()
  const profile = makeProfile()
  const dir = makeTemp('.tmp-tamper-migrate-')
  const logs = path.join(dir, 'logs')
  const journal = path.join(dir, 'journal')
  const state = path.join(dir, 'state')
  fs.mkdirSync(logs, { recursive: true })
  fs.mkdirSync(journal, { recursive: true })
  fs.mkdirSync(state, { recursive: true })
  fs.writeFileSync(
    path.join(state, 'state.json'),
    JSON.stringify({
      startedAt: new Date().toISOString(),
      lastCrashScan: 0,
      incidents: [],
      escalation: { crashes: [], level: 'L0' },
      budgets: {},
      quarantine: [{ target: '@twinsearth/fang-dsh-plugin', reason: 'tamper', level: 'L1', at: new Date().toISOString(), release: 'manual', evidence: 'old false positive' }],
      migratedTamperQuarantine: false,
    }),
  )
  const { ctx } = makeCtx()
  const watchdog = createWatchdog(
    ctx,
    { profileDir: profile, dataDir: state, logsDir: logs, journalDir: journal, sampleMs: 60_000, tamperMonitor: false },
    { info() {}, warn() {} },
  )
  try {
    const snapshot = watchdog.snapshot()
    assert.equal(snapshot.quarantine.length, 0)
    assert.ok(snapshot.incidents.some((incident) => incident.kind === 'migration'))
  } finally {
    watchdog.dispose()
    fs.rmSync(profile, { recursive: true, force: true })
    fs.rmSync(dir, { recursive: true, force: true })
  }
})

test('observe policy records the drift but never restores or jails', async () => {
  resetWatchdogState()
  const profile = makeProfile()
  const dirs = { dir: makeTemp('.tmp-tamper-observe-') }
  dirs.logs = path.join(dirs.dir, 'logs')
  dirs.journal = path.join(dirs.dir, 'journal')
  dirs.state = path.join(dirs.dir, 'state')
  fs.mkdirSync(dirs.logs, { recursive: true })
  fs.mkdirSync(dirs.journal, { recursive: true })
  fs.mkdirSync(dirs.state, { recursive: true })
  const { ctx } = makeCtx()
  const watchdog = createWatchdog(
    ctx,
    { profileDir: profile, dataDir: dirs.state, logsDir: dirs.logs, journalDir: dirs.journal, sampleMs: 60_000, tamperIntervalMs: 0, tamperGraceMs: 0, tamperPolicy: 'observe' },
    { info() {}, warn() {} },
  )
  try {
    fs.writeFileSync(path.join(profile, 'package.json'), '{"name":"observed"}')
    const result = await watchdog.tamperCheck({ force: true })
    assert.equal(result.restored.length, 0)
    assert.equal(JSON.parse(fs.readFileSync(path.join(profile, 'package.json'), 'utf8')).name, 'observed')
    const snapshot = watchdog.snapshot()
    const incident = snapshot.incidents.find((entry) => entry.kind === 'tamper')
    assert.equal(incident.policy, 'observe')
    assert.equal(snapshot.quarantine.length, 0)
    const plan = snapshot.plans.find((entry) => entry.reason === 'tamper')
    assert.equal(plan.applied, false)
  } finally {
    watchdog.dispose()
    fs.rmSync(profile, { recursive: true, force: true })
    fs.rmSync(dirs.dir, { recursive: true, force: true })
  }
})
