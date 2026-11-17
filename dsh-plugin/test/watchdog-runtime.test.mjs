import assert from 'node:assert/strict'
import fs from 'node:fs'
import path from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'
import { createWatchdog } from '../lib/watchdog-runtime.js'
import { resetWatchdogState } from '../lib/watchdog-state.js'

const pluginRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')

function makeCtx(overrides = {}) {
  const state = { operationHandler: null, effects: [] }
  const ctx = {
    effect: (fn) => {
      const disposer = fn()
      if (typeof disposer === 'function') state.effects.push(disposer)
      return disposer
    },
    on: (name, handler) => {
      if (name === 'plugin-manager/operation') state.operationHandler = handler
      return () => {}
    },
    inject: (deps, callback) => {
      const child = { effect: (fn) => fn(), ...overrides }
      if (deps.includes('pluginManager') && overrides.pluginManager) callback(child)
      if (deps.includes('tools') && overrides.tools) callback(child)
    },
    ...overrides.ctx,
  }
  return { ctx, state }
}

const CRASH_FIXTURE = `time: 2026-01-01T00:00:00.000Z
source: host
phase: running
app: @deepseek-ai/dsh-desktop 0.2.2-rc.3

--- error ---
Error: dsh desktop host exited with 134: boom
  hostRunId: 'run-x'
FATAL ERROR: MarkCompactCollector: young object promotion failed Allocation failed - JavaScript heap out of memory`

function makeDirs() {
  const dir = fs.mkdtempSync(path.join(pluginRoot, '.tmp-watchdog-'))
  const logs = path.join(dir, 'logs')
  const journal = path.join(dir, 'journal')
  const state = path.join(dir, 'state')
  fs.mkdirSync(logs, { recursive: true })
  fs.mkdirSync(journal, { recursive: true })
  return { dir, logs, journal, state }
}

test('watchdog runs in enforce mode and samples the heap', () => {
  resetWatchdogState()
  const dirs = makeDirs()
  const { ctx } = makeCtx()
  const watchdog = createWatchdog(ctx, { dataDir: dirs.state, logsDir: dirs.logs, journalDir: dirs.journal, sampleMs: 60_000 }, { info() {}, warn() {} })
  try {
    const snapshot = watchdog.snapshot()
    assert.equal(snapshot.mode, 'enforce')
    assert.equal(snapshot.enforce, true)
    assert.ok(snapshot.heap.heapUsed > 0)
    assert.ok(snapshot.thresholds.totalBlock > 0)
    assert.equal(snapshot.waterline, 'ok')
  } finally {
    watchdog.dispose()
    fs.rmSync(dirs.dir, { recursive: true, force: true })
  }
})

test('watchdog can run in observe mode without enforcement', () => {
  resetWatchdogState()
  const dirs = makeDirs()
  const { ctx } = makeCtx()
  const watchdog = createWatchdog(ctx, { dataDir: dirs.state, logsDir: dirs.logs, journalDir: dirs.journal, sampleMs: 60_000, enforce: false }, { info() {}, warn() {} })
  try {
    const snapshot = watchdog.snapshot()
    assert.equal(snapshot.mode, 'observe')
    assert.equal(snapshot.enforce, false)
  } finally {
    watchdog.dispose()
    fs.rmSync(dirs.dir, { recursive: true, force: true })
  }
})

test('crash attribution records a dry-run L1 plan from log + journal', () => {
  resetWatchdogState()
  const dirs = makeDirs()
  fs.writeFileSync(path.join(dirs.logs, 'crash-2026-01-01T00-00-00-000Z-host.log'), CRASH_FIXTURE)
  fs.writeFileSync(
    path.join(dirs.journal, 'operations.jsonl'),
    [
      { runId: 'run-x', operation: { id: 'op-1', time: '2026-01-01T00:00:00.000Z', reason: 'bundle', target: '@twinsearth/dsh-w2m', stage: 'enable', enabled: true, version: '3.5.0', phase: 'started' } },
      { runId: 'run-x', operation: { id: 'op-2', time: '2025-12-31T23:58:00.000Z', reason: 'install', target: '@a/installed', stage: 'install', enabled: true, phase: 'settled', application: 'applied', changed: true } },
    ]
      .map((record) => JSON.stringify(record))
      .join('\n') + '\n',
  )
  const { ctx } = makeCtx()
  const watchdog = createWatchdog(ctx, { dataDir: dirs.state, logsDir: dirs.logs, journalDir: dirs.journal, sampleMs: 60_000 }, { info() {}, warn() {} })
  try {
    const snapshot = watchdog.snapshot()
    const plan = snapshot.plans[0]
    assert.ok(plan)
    assert.equal(plan.level, 'L1')
    assert.equal(plan.target, '@twinsearth/dsh-w2m')
    assert.equal(plan.inFlight, true)
    assert.equal(snapshot.escalation.level, 'L1')
    const incident = snapshot.incidents.find((entry) => entry.kind === 'crash')
    assert.ok(incident)
    assert.equal(incident.target, '@twinsearth/dsh-w2m')
  } finally {
    watchdog.dispose()
    fs.rmSync(dirs.dir, { recursive: true, force: true })
  }
})

test('six hot operations auto-close and quarantine the target', async () => {
  resetWatchdogState()
  const dirs = makeDirs()
  const calls = []
  const manager = {
    async listBundles() {
      return [{ name: '@x/y', version: '1.0.0', enabled: true }]
    },
    async setBundleEnabled(target, enabled) {
      calls.push({ target, enabled })
      return { stage: 'enable', target, enabled, changed: true, application: 'applied' }
    },
  }
  const { ctx, state } = makeCtx({ pluginManager: manager })
  const watchdog = createWatchdog(ctx, { dataDir: dirs.state, logsDir: dirs.logs, journalDir: dirs.journal, sampleMs: 60_000 }, { info() {}, warn() {} })
  try {
    assert.equal(typeof state.operationHandler, 'function')
    const time = new Date().toISOString()
    for (let index = 0; index < 6; index += 1) {
      const id = `op-${index}`
      state.operationHandler({ id, time, reason: 'bundle', target: '@x/y', stage: 'enable', enabled: true, version: '1.0.0', phase: 'started' })
      state.operationHandler({ id, time, reason: 'bundle', target: '@x/y', stage: 'enable', enabled: true, version: '1.0.0', phase: 'settled', application: 'applied', changed: true })
      await new Promise((resolve) => setTimeout(resolve, 0))
    }
    await new Promise((resolve) => setTimeout(resolve, 20))
    const snapshot = watchdog.snapshot()
    const row = snapshot.operations.find((entry) => entry.target === '@x/y')
    assert.equal(row.count, 6)
    assert.ok(calls.some((call) => call.target === '@x/y' && call.enabled === false), 'auto-disable called')
    const jailed = snapshot.quarantine.find((entry) => entry.target === '@x/y')
    assert.ok(jailed)
    assert.equal(jailed.reason, 'hot-ops')
    const released = watchdog.releaseTarget('@x/y')
    assert.equal(released.ok, true)
    assert.equal(watchdog.snapshot().quarantine.length, 0)
  } finally {
    watchdog.dispose()
    fs.rmSync(dirs.dir, { recursive: true, force: true })
  }
})

test('operations on exempt targets never quarantine', async () => {
  resetWatchdogState()
  const dirs = makeDirs()
  const calls = []
  const manager = {
    async listBundles() {
      return [{ name: 'dsh-web-app', version: '1.0.0', enabled: true }]
    },
    async setBundleEnabled(target, enabled) {
      calls.push({ target, enabled })
    },
  }
  const { ctx, state } = makeCtx({ pluginManager: manager })
  const watchdog = createWatchdog(ctx, { dataDir: dirs.state, logsDir: dirs.logs, journalDir: dirs.journal, sampleMs: 60_000 }, { info() {}, warn() {} })
  try {
    const time = new Date().toISOString()
    for (let index = 0; index < 8; index += 1) {
      const id = `op-${index}`
      state.operationHandler({ id, time, reason: 'bundle', target: 'dsh-web-app', stage: 'enable', enabled: true, phase: 'started' })
      state.operationHandler({ id, time, reason: 'bundle', target: 'dsh-web-app', stage: 'enable', enabled: true, phase: 'settled', application: 'applied', changed: true })
    }
    await new Promise((resolve) => setTimeout(resolve, 20))
    const snapshot = watchdog.snapshot()
    assert.equal(snapshot.quarantine.length, 0)
    assert.equal(calls.length, 0)
  } finally {
    watchdog.dispose()
    fs.rmSync(dirs.dir, { recursive: true, force: true })
  }
})
