import assert from 'node:assert/strict'
import fs from 'node:fs'
import path from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'
import { createWatchdog } from '../lib/watchdog-runtime.js'
import { resetWatchdogState } from '../lib/watchdog-state.js'

const pluginRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')

function makeDirs() {
  const dir = fs.mkdtempSync(path.join(pluginRoot, '.tmp-failure-'))
  const dirs = {
    dir,
    logs: path.join(dir, 'logs'),
    journal: path.join(dir, 'journal'),
    state: path.join(dir, 'state'),
  }
  for (const sub of ['logs', 'journal', 'state']) fs.mkdirSync(dirs[sub], { recursive: true })
  return dirs
}

function writeCrash(dirs, name, { runId, phase = 'running', exit = 134, oom = true }) {
  const lines = [
    'time: 2026-01-01T00:00:00.000Z',
    'source: host',
    `phase: ${phase}`,
    '',
    '--- error ---',
    `Error: dsh desktop host exited with ${exit}: boom`,
    `  hostRunId: '${runId}'`,
  ]
  if (oom) lines.push('FATAL ERROR: MarkCompactCollector: young object promotion failed Allocation failed - JavaScript heap out of memory')
  fs.writeFileSync(path.join(dirs.logs, name), lines.join('\n'))
}

function writeJournal(dirs, records) {
  fs.writeFileSync(path.join(dirs.journal, 'operations.jsonl'), records.map((record) => JSON.stringify(record)).join('\n') + '\n')
}

function makeManager(rows, calls) {
  return {
    async listBundles() {
      return rows.map((row) => ({ ...row }))
    },
    async setBundleEnabled(target, enabled) {
      calls.push({ target, enabled })
      return { target, enabled, application: 'applied', changed: true }
    },
  }
}

function makeCtx(manager) {
  const handlers = {}
  return {
    ctx: {
      effect: (fn) => {
        const disposer = fn()
        return typeof disposer === 'function' ? disposer : () => {}
      },
      on: (name, handler) => {
        handlers[name] = handler
        return () => {}
      },
      inject: (deps, callback) => {
        if (deps.includes('pluginManager')) callback({ pluginManager: manager, effect: (fn) => fn() })
        if (deps.includes('tools')) callback({ tools: { guard: () => () => {} }, effect: (fn) => fn() })
      },
    },
    handlers,
  }
}

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms))

test('a startup OOM is attributed and enforced (close + quarantine + pending-boot)', async () => {
  resetWatchdogState()
  const dirs = makeDirs()
  writeCrash(dirs, 'crash-2026-01-01T00-00-01-000Z-host.log', { runId: 'run-startup', phase: 'startup' })
  writeJournal(dirs, [
    { runId: 'run-startup', operation: { id: 'op-1', time: '2026-01-01T00:00:00.500Z', reason: 'bundle', target: '@x/startup', stage: 'enable', enabled: true, version: '1.0.0', phase: 'started' } },
  ])
  const calls = []
  const manager = makeManager([{ name: '@x/startup', version: '1.0.0', enabled: true }], calls)
  const { ctx } = makeCtx(manager)
  const watchdog = createWatchdog(ctx, { dataDir: dirs.state, logsDir: dirs.logs, journalDir: dirs.journal, sampleMs: 60_000 }, { info() {}, warn() {} })
  try {
    await sleep(150)
    const snapshot = watchdog.snapshot()
    const plan = snapshot.plans[0]
    assert.equal(plan.crash.phase, 'startup')
    assert.equal(plan.crash.oom, true)
    assert.equal(plan.target, '@x/startup')
    assert.equal(plan.level, 'L1')
    assert.ok(calls.some((call) => call.target === '@x/startup' && call.enabled === false), 'the attributed plugin is closed')
    assert.ok(snapshot.quarantine.find((entry) => entry.target === '@x/startup'))
    const pending = JSON.parse(fs.readFileSync(path.join(dirs.state, 'pending-boot.json'), 'utf8'))
    assert.ok(pending.disable.includes('@x/startup'))
    assert.ok(snapshot.incidents.some((incident) => incident.kind === 'crash-enforced'))
    assert.equal(snapshot.pendingBoot.exists, true)
    assert.equal(snapshot.pendingBoot.consumer, 'none')
    assert.equal(snapshot.pendingBoot.appliesNextBoot, false)
    assert.match(snapshot.pendingBoot.hint, /人工处理/)
  } finally {
    watchdog.dispose()
    fs.rmSync(dirs.dir, { recursive: true, force: true })
  }
})

test('a non-OOM exit-1 host crash is parsed and attributed without OOM flags', async () => {
  resetWatchdogState()
  const dirs = makeDirs()
  writeCrash(dirs, 'crash-2026-01-01T00-00-02-000Z-host.log', { runId: 'run-exit', phase: 'running', exit: 1, oom: false })
  writeJournal(dirs, [
    { runId: 'run-exit', operation: { id: 'op-1', time: '2026-01-01T00:00:00.500Z', reason: 'bundle', target: '@x/exit', stage: 'enable', enabled: true, version: '1.0.0', phase: 'started' } },
  ])
  const calls = []
  const manager = makeManager([{ name: '@x/exit', version: '1.0.0', enabled: true }], calls)
  const { ctx } = makeCtx(manager)
  const watchdog = createWatchdog(ctx, { dataDir: dirs.state, logsDir: dirs.logs, journalDir: dirs.journal, sampleMs: 60_000 }, { info() {}, warn() {} })
  try {
    await sleep(150)
    const snapshot = watchdog.snapshot()
    const plan = snapshot.plans[0]
    assert.equal(plan.crash.oom, false)
    assert.equal(plan.crash.hostExitCode, 1)
    assert.equal(plan.target, '@x/exit')
    assert.ok(calls.some((call) => call.target === '@x/exit' && call.enabled === false))
  } finally {
    watchdog.dispose()
    fs.rmSync(dirs.dir, { recursive: true, force: true })
  }
})

test('three crashes inside the window escalate to L3 and close non-baseline plugins', async () => {
  resetWatchdogState()
  const dirs = makeDirs()
  for (let index = 1; index <= 3; index += 1) {
    writeCrash(dirs, `crash-2026-01-01T00-00-0${index}-000Z-host.log`, { runId: `run-${index}` })
  }
  writeJournal(dirs, [
    { runId: 'run-3', operation: { id: 'op-3', time: '2026-01-01T00:00:03.500Z', reason: 'bundle', target: '@x/third', stage: 'enable', enabled: true, version: '1.0.0', phase: 'started' } },
  ])
  const calls = []
  const manager = makeManager([
    { name: '@x/third', version: '1.0.0', enabled: true },
    { name: '@x/extra', version: '1.0.0', enabled: true },
  ], calls)
  const { ctx } = makeCtx(manager)
  const watchdog = createWatchdog(ctx, { dataDir: dirs.state, logsDir: dirs.logs, journalDir: dirs.journal, sampleMs: 60_000 }, { info() {}, warn() {} })
  try {
    await sleep(800)
    const snapshot = watchdog.snapshot()
    assert.equal(snapshot.escalation.level, 'L3')
    assert.equal(snapshot.plans.at(-1).level, 'L3')
    const closed = new Set(calls.filter((call) => call.enabled === false).map((call) => call.target))
    assert.ok(closed.has('@x/extra'), 'L3 closes non-baseline plugins')
    const pending = JSON.parse(fs.readFileSync(path.join(dirs.state, 'pending-boot.json'), 'utf8'))
    assert.ok(pending.level === 'L3')
  } finally {
    watchdog.dispose()
    fs.rmSync(dirs.dir, { recursive: true, force: true })
  }
})

test('observe mode records the same crash evidence but never closes a plugin', async () => {
  resetWatchdogState()
  const dirs = makeDirs()
  writeCrash(dirs, 'crash-2026-01-01T00-00-04-000Z-host.log', { runId: 'run-observe' })
  writeJournal(dirs, [
    { runId: 'run-observe', operation: { id: 'op-1', time: '2026-01-01T00:00:00.500Z', reason: 'bundle', target: '@x/observed', stage: 'enable', enabled: true, version: '1.0.0', phase: 'started' } },
  ])
  const calls = []
  const manager = makeManager([{ name: '@x/observed', version: '1.0.0', enabled: true }], calls)
  const { ctx } = makeCtx(manager)
  const watchdog = createWatchdog(ctx, { dataDir: dirs.state, logsDir: dirs.logs, journalDir: dirs.journal, sampleMs: 60_000, enforce: false }, { info() {}, warn() {} })
  try {
    await sleep(150)
    const snapshot = watchdog.snapshot()
    assert.equal(snapshot.mode, 'observe')
    assert.equal(snapshot.plans[0].target, '@x/observed')
    assert.equal(calls.length, 0, 'observe mode never calls setBundleEnabled')
    assert.equal(fs.existsSync(path.join(dirs.state, 'pending-boot.json')), false)
    assert.equal(snapshot.pendingBoot.exists, false)
  } finally {
    watchdog.dispose()
    fs.rmSync(dirs.dir, { recursive: true, force: true })
  }
})
