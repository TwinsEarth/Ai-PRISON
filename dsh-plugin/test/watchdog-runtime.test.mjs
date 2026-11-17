import assert from 'node:assert/strict'
import fs from 'node:fs'
import path from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'
import { createWatchdog } from '../lib/watchdog-runtime.js'
import { resetWatchdogState } from '../lib/watchdog-state.js'

const pluginRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')

function makeCtx() {
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
  }
  return { ctx, state }
}

const CRASH_FIXTURE = `time: 2026-01-01T00:00:00.000Z
source: host
phase: running
app: @deepseek-ai/dsh-desktop 0.2.2-rc.3

--- error ---
Error: dsh desktop host exited with 134: boom
FATAL ERROR: MarkCompactCollector: young object promotion failed Allocation failed - JavaScript heap out of memory`

test('watchdog samples the heap, records crash fixtures and persists state', () => {
  resetWatchdogState()
  const dir = fs.mkdtempSync(path.join(pluginRoot, '.tmp-watchdog-'))
  const logs = path.join(dir, 'logs')
  const stateDir = path.join(dir, 'state')
  fs.mkdirSync(logs, { recursive: true })
  fs.writeFileSync(path.join(logs, 'crash-2026-01-01T00-00-00-000Z-host.log'), CRASH_FIXTURE)
  const { ctx } = makeCtx()
  const watchdog = createWatchdog(ctx, { dataDir: stateDir, logsDir: logs, sampleMs: 60_000 }, { info() {}, warn() {} })
  try {
    const snapshot = watchdog.snapshot()
    assert.equal(snapshot.ok, true)
    assert.equal(snapshot.mode, 'observe')
    assert.ok(snapshot.heap.heapUsed > 0)
    assert.ok(snapshot.heap.heapLimit > 0)
    assert.ok(snapshot.thresholds.totalBlock > 0)
    assert.ok(snapshot.thresholds.opBlock <= snapshot.heap.heapLimit)
    assert.equal(snapshot.waterline, 'ok')
    const crash = snapshot.incidents.find((incident) => incident.kind === 'crash')
    assert.ok(crash, 'crash fixture recorded')
    assert.equal(crash.oom, true)
    assert.equal(crash.hostExitCode, 134)
    assert.ok(fs.existsSync(path.join(stateDir, 'state.json')))
    const persisted = JSON.parse(fs.readFileSync(path.join(stateDir, 'state.json'), 'utf8'))
    assert.ok(persisted.lastCrashScan > 0)
  } finally {
    watchdog.dispose()
    fs.rmSync(dir, { recursive: true, force: true })
  }
})

test('watchdog counts plugin operations and keeps a per-target estimate', () => {
  resetWatchdogState()
  const dir = fs.mkdtempSync(path.join(pluginRoot, '.tmp-watchdog-'))
  const { ctx, state } = makeCtx()
  const watchdog = createWatchdog(
    ctx,
    { dataDir: path.join(dir, 'state'), logsDir: path.join(dir, 'missing-logs'), sampleMs: 60_000 },
    { info() {}, warn() {} },
  )
  try {
    assert.equal(typeof state.operationHandler, 'function', 'operation handler subscribed')
    const time = new Date().toISOString()
    state.operationHandler({ id: 'op-1', time, reason: 'bundle', target: '@x/y', stage: 'enable', enabled: true, version: '1.0.0', phase: 'started' })
    state.operationHandler({ id: 'op-1', time, reason: 'bundle', target: '@x/y', stage: 'enable', enabled: true, version: '1.0.0', phase: 'settled', application: 'applied', changed: true })
    state.operationHandler({ id: 'op-2', time, reason: 'bundle', target: '@x/y', stage: 'enable', enabled: true, phase: 'started' })
    state.operationHandler({ id: 'op-2', time, reason: 'bundle', target: '@x/y', stage: 'enable', enabled: true, phase: 'settled', application: 'applied', changed: true })
    const snapshot = watchdog.snapshot()
    const row = snapshot.operations.find((entry) => entry.target === '@x/y')
    assert.ok(row)
    assert.equal(row.count, 2)
    assert.ok(row.estimateMB >= 0)
    assert.equal(snapshot.mode, 'observe')
  } finally {
    watchdog.dispose()
    fs.rmSync(dir, { recursive: true, force: true })
  }
})

test('watchdog ignores failed operations and removal deletes the target row', () => {
  resetWatchdogState()
  const dir = fs.mkdtempSync(path.join(pluginRoot, '.tmp-watchdog-'))
  const { ctx, state } = makeCtx()
  const watchdog = createWatchdog(
    ctx,
    { dataDir: path.join(dir, 'state'), logsDir: path.join(dir, 'missing-logs'), sampleMs: 60_000 },
    { info() {}, warn() {} },
  )
  try {
    const time = new Date().toISOString()
    state.operationHandler({ id: 'op-f', time, reason: 'bundle', target: '@fail/plugin', stage: 'enable', enabled: true, phase: 'started' })
    state.operationHandler({ id: 'op-f', time, reason: 'bundle', target: '@fail/plugin', stage: 'enable', enabled: true, phase: 'settled', application: 'failed', changed: false })
    let snapshot = watchdog.snapshot()
    const failedRow = snapshot.operations.find((entry) => entry.target === '@fail/plugin')
    assert.ok(failedRow)
    assert.equal(failedRow.count, 0)
    state.operationHandler({ id: 'op-d', time, reason: 'remove', target: '@fail/plugin', stage: 'remove', phase: 'started' })
    state.operationHandler({ id: 'op-d', time, reason: 'remove', target: '@fail/plugin', stage: 'remove', phase: 'settled', application: 'applied', changed: true })
    snapshot = watchdog.snapshot()
    assert.equal(snapshot.operations.some((entry) => entry.target === '@fail/plugin'), false)
  } finally {
    watchdog.dispose()
    fs.rmSync(dir, { recursive: true, force: true })
  }
})
