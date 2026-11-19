import assert from 'node:assert/strict'
import fs from 'node:fs'
import path from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'
import { createWatchdog } from '../lib/watchdog-runtime.js'
import { resetWatchdogState, watchdogState } from '../lib/watchdog-state.js'

const pluginRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')

function makeDirs() {
  const dir = fs.mkdtempSync(path.join(pluginRoot, '.tmp-crashplan-'))
  const dirs = { dir, logs: path.join(dir, 'logs'), journal: path.join(dir, 'journal'), state: path.join(dir, 'state') }
  for (const sub of ['logs', 'journal', 'state']) fs.mkdirSync(dirs[sub], { recursive: true })
  return dirs
}

function writeCrash(dirs, name, { runId, phase = 'running', exit = 134, oom = true, at = '2026-01-01T00:00:00.000Z' } = {}) {
  const lines = ['time: ' + at, 'source: host', 'phase: ' + phase, '', '--- error ---', 'Error: dsh desktop host exited with ' + exit + ': boom', "  hostRunId: '" + runId + "'"]
  if (oom) lines.push('FATAL ERROR: MarkCompactCollector: young object promotion failed Allocation failed - JavaScript heap out of memory')
  fs.writeFileSync(path.join(dirs.logs, name), lines.join('\n'))
}

function writeJournal(dirs, records) {
  fs.writeFileSync(path.join(dirs.journal, 'operations.jsonl'), records.map((record) => JSON.stringify(record)).join('\n') + '\n')
}

function makeCtx(manager) {
  return {
    effect: (fn) => {
      const disposer = fn()
      return typeof disposer === 'function' ? disposer : () => {}
    },
    on: () => () => {},
    inject: (deps, callback) => {
      if (deps.includes('pluginManager')) callback({ pluginManager: manager, effect: (fn) => fn() })
      if (deps.includes('tools')) callback({ tools: { guard: () => () => {} }, effect: (fn) => fn() })
    },
  }
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

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms))

test('snapshot exposes the crash-plan preview and an unknown first crash only records', async () => {
  resetWatchdogState()
  const dirs = makeDirs()
  const calls = []
  writeCrash(dirs, 'crash-2026-01-01T00-00-01-000Z-host.log', { runId: 'run-unknown' })
  const { ctx } = { ctx: makeCtx(makeManager([], calls)) }
  const watchdog = createWatchdog(ctx, { dataDir: dirs.state, logsDir: dirs.logs, journalDir: dirs.journal, sampleMs: 60_000 }, { info() {}, warn() {} })
  try {
    await sleep(150)
    const snapshot = watchdog.snapshot()
    assert.equal(snapshot.crashPlan.level, 'L1')
    assert.equal(snapshot.crashPlan.crashes, 1)
    assert.equal(snapshot.crashPlan.nextLevel, 'L2')
    assert.equal(snapshot.crashPlan.resetMs, 30 * 60_000)
    assert.deepEqual(snapshot.crashPlan.steps, ['isolate-target'])
    const plan = snapshot.plans.at(-1)
    assert.equal(plan.level, 'L1')
    assert.equal(plan.target, null)
    assert.equal(plan.fallback, 'observe-only')
    assert.equal(plan.applied, false)
    assert.equal(calls.length, 0)
    assert.ok(snapshot.incidents.some((incident) => incident.kind === 'crash-escalation' && incident.level === 'L1'))
    assert.equal(snapshot.crashPlan.lastPlan.fallback, 'observe-only')
  } finally {
    watchdog.dispose()
    fs.rmSync(dirs.dir, { recursive: true, force: true })
  }
})

test('two crashes escalate to L2 with changed-set isolation and a pending-boot plan', async () => {
  resetWatchdogState()
  const dirs = makeDirs()
  writeCrash(dirs, 'crash-2026-01-01T00-00-01-000Z-host.log', { runId: 'run-1', at: '2026-01-01T00:00:01.000Z' })
  writeCrash(dirs, 'crash-2026-01-01T00-00-02-000Z-host.log', { runId: 'run-2', at: '2026-01-01T00:00:02.000Z' })
  writeJournal(dirs, [
    { runId: 'run-2', operation: { id: 'op-2', time: '2026-01-01T00:00:02.500Z', reason: 'bundle', target: '@x/second', stage: 'enable', enabled: true, version: '1.0.0', phase: 'started' } },
  ])
  const calls = []
  const manager = makeManager([
    { name: '@x/second', version: '1.0.0', enabled: true },
    { name: '@x/other', version: '1.0.0', enabled: true },
    { name: '@deepseek-ai/dsh-base', version: '1.0.0', enabled: true, official: true },
  ], calls)
  const watchdog = createWatchdog(makeCtx(manager), { dataDir: dirs.state, logsDir: dirs.logs, journalDir: dirs.journal, sampleMs: 60_000 }, { info() {}, warn() {} })
  try {
    await sleep(1200)
    const snapshot = watchdog.snapshot()
    assert.equal(snapshot.crashPlan.level, 'L2')
    const plan = snapshot.plans.at(-1)
    assert.equal(plan.level, 'L2')
    assert.equal(plan.target, '@x/second')
    assert.ok(plan.targets.includes('@x/second'))
    assert.equal(plan.applied, true)
    const pending = JSON.parse(fs.readFileSync(path.join(dirs.state, 'pending-boot.json'), 'utf8'))
    assert.equal(pending.level, 'L2')
    assert.ok(pending.disable.includes('@x/second'))
    const closed = calls.filter((call) => call.enabled === false).map((call) => call.target)
    assert.ok(closed.includes('@x/second'))
  } finally {
    watchdog.dispose()
    fs.rmSync(dirs.dir, { recursive: true, force: true })
  }
})

test('thirty stable minutes reset the escalation counters before a new crash', async () => {
  resetWatchdogState()
  const dirs = makeDirs()
  const watchdog = createWatchdog(makeCtx(makeManager([], [])), { dataDir: dirs.state, logsDir: dirs.logs, journalDir: dirs.journal, sampleMs: 60_000 }, { info() {}, warn() {} })
  try {
    const now = Date.now()
    watchdogState.escalation = { crashes: [now, now], level: 'L2', previousLevel: 'L1', lastCrashAt: now - 31 * 60_000 }
    writeCrash(dirs, 'crash-2026-01-01T00-00-09-000Z-host.log', { runId: 'run-reset' })
    await watchdog.readCrashLogs()
    await sleep(50)
    assert.equal(watchdogState.escalation.level, 'L1')
    assert.equal(watchdogState.escalation.crashes.length, 1)
    assert.ok(watchdog.snapshot().incidents.some((incident) => incident.kind === 'crash-reset'))
  } finally {
    watchdog.dispose()
    fs.rmSync(dirs.dir, { recursive: true, force: true })
  }
})
