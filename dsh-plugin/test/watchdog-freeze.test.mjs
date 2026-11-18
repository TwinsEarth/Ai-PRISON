import assert from 'node:assert/strict'
import fs from 'node:fs'
import path from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'
import { createWatchdog } from '../lib/watchdog-runtime.js'
import { resetWatchdogState, watchdogState } from '../lib/watchdog-state.js'
import { thresholdsFor, GB } from '../lib/watchdog-policy.js'

const pluginRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')

function setup() {
  resetWatchdogState()
  const dir = fs.mkdtempSync(path.join(pluginRoot, '.tmp-freeze-'))
  const logs = path.join(dir, 'logs')
  const journal = path.join(dir, 'journal')
  const state = path.join(dir, 'state')
  fs.mkdirSync(logs, { recursive: true })
  fs.mkdirSync(journal, { recursive: true })
  fs.mkdirSync(state, { recursive: true })
  const calls = []
  const manager = {
    async listBundles() {
      return [{ name: '@x/target', version: '1.0.0', enabled: true }]
    },
    async setBundleEnabled(target, enabled) {
      calls.push({ target, enabled })
      return { application: 'applied', changed: true }
    },
  }
  const handlers = {}
  const ctx = {
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
  }
  const watchdog = createWatchdog(
    ctx,
    {
      profileDir: dir,
      dataDir: state,
      logsDir: logs,
      journalDir: journal,
      sampleMs: 60_000,
      tamperMonitor: false,
    },
    { info() {}, warn() {} },
  )
  return { watchdog, manager, calls, handlers, dir }
}

function emit(handlers, overrides = {}) {
  const operation = {
    id: 'op-1',
    time: new Date().toISOString(),
    reason: 'bundle',
    target: '@x/target',
    stage: 'enable',
    enabled: true,
    version: '1.0.0',
    phase: 'settled',
    application: 'applied',
    changed: true,
    ...overrides,
  }
  handlers['plugin-manager/operation'](operation)
}

test('a frozen host rolls back an additive operation and records it', async () => {
  const { watchdog, calls, handlers, dir } = setup()
  try {
    watchdogState.waterline = 'frost'
    watchdogState.frozenAt = new Date().toISOString()
    watchdogState.thresholds = thresholdsFor(6 * GB)
    watchdogState.graceUntil = null
    emit(handlers)
    await new Promise((resolve) => setTimeout(resolve, 20))
    assert.ok(calls.some((call) => call.target === '@x/target' && call.enabled === false), 'rollback disable called')
    const snapshot = watchdog.snapshot()
    assert.ok(snapshot.incidents.some((incident) => incident.kind === 'freeze-rollback' && incident.target === '@x/target'))
  } finally {
    watchdog.dispose()
    fs.rmSync(dir, { recursive: true, force: true })
  }
})

test('a paused host without grace also rolls back, with grace it does not', async () => {
  const { watchdog, calls, handlers, dir } = setup()
  try {
    watchdogState.waterline = 'pause'
    watchdogState.pausedAt = new Date().toISOString()
    watchdogState.thresholds = thresholdsFor(6 * GB)
    watchdogState.graceUntil = null
    emit(handlers, { id: 'op-pause' })
    await new Promise((resolve) => setTimeout(resolve, 20))
    assert.ok(calls.some((call) => call.target === '@x/target' && call.enabled === false), 'pause rollback called')

    calls.length = 0
    watchdogState.graceUntil = Date.now() + 60_000
    emit(handlers, { id: 'op-grace' })
    await new Promise((resolve) => setTimeout(resolve, 20))
    assert.equal(calls.length, 0, 'graced operation is allowed')
  } finally {
    watchdog.dispose()
    fs.rmSync(dir, { recursive: true, force: true })
  }
})

test('release-class operations are never rolled back while frozen', async () => {
  const { watchdog, calls, handlers, dir } = setup()
  try {
    watchdogState.waterline = 'frost'
    watchdogState.frozenAt = new Date().toISOString()
    watchdogState.thresholds = thresholdsFor(6 * GB)
    emit(handlers, { id: 'op-disable', stage: 'enable', enabled: false })
    emit(handlers, { id: 'op-remove', stage: 'remove', target: '@x/target' })
    await new Promise((resolve) => setTimeout(resolve, 20))
    assert.equal(calls.length, 0)
    assert.equal(watchdog.snapshot().incidents.filter((incident) => incident.kind === 'freeze-rollback').length, 0)
  } finally {
    watchdog.dispose()
    fs.rmSync(dir, { recursive: true, force: true })
  }
})
