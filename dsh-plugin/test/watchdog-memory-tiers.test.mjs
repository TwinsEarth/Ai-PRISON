import assert from 'node:assert/strict'
import fs from 'node:fs'
import path from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'
import { createWatchdog } from '../lib/watchdog-runtime.js'
import { decideToolGuard, memoryTierFor, MEMORY_TIER_MB } from '../lib/watchdog-policy.js'
import { resetWatchdogState, watchdogState } from '../lib/watchdog-state.js'

const pluginRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const tick = (ms = 40) => new Promise((resolve) => setTimeout(resolve, ms))

function makeDirs() {
  const dir = fs.mkdtempSync(path.join(pluginRoot, '.tmp-tiers-'))
  const dirs = { dir, logs: path.join(dir, 'logs'), journal: path.join(dir, 'journal'), state: path.join(dir, 'state') }
  for (const sub of ['logs', 'journal', 'state']) fs.mkdirSync(dirs[sub], { recursive: true })
  return dirs
}

function makeManager(rows) {
  const calls = []
  const removed = []
  return {
    calls,
    removed,
    rows,
    async listBundles() {
      return this.rows.map((row) => ({ ...row }))
    },
    async setBundleEnabled(target, enabled) {
      this.calls.push({ target, enabled })
      return { target, enabled, application: 'applied', changed: true }
    },
    async removeBundle(target) {
      this.removed.push(target)
      return { target, application: 'applied', changed: true, stage: 'remove' }
    },
  }
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

function makeWatchdog(rows) {
  resetWatchdogState()
  const dirs = makeDirs()
  const manager = makeManager(rows)
  const watchdog = createWatchdog(makeCtx(manager), { dataDir: dirs.state, logsDir: dirs.logs, journalDir: dirs.journal, sampleMs: 60_000 }, { info() {}, warn() {} })
  return { watchdog, manager, dirs }
}

test('memory tier thresholds map to the five plugin-prison rules', () => {
  assert.equal(memoryTierFor(0), 'ok')
  assert.equal(memoryTierFor(299), 'ok')
  assert.equal(memoryTierFor(MEMORY_TIER_MB.warn), 'warn')
  assert.equal(memoryTierFor(MEMORY_TIER_MB.throttle), 'throttle')
  assert.equal(memoryTierFor(MEMORY_TIER_MB.close), 'close')
  assert.equal(memoryTierFor(MEMORY_TIER_MB.uninstall), 'uninstall')
  assert.equal(memoryTierFor(MEMORY_TIER_MB.allThirdParty), 'all-third-party')
})

test('300 MB warns and 500 MB throttles with a tool-guard denial', async () => {
  const { watchdog } = makeWatchdog([
    { name: '@x/warn', version: '1.0.0', enabled: true, official: false },
    { name: '@x/throttle', version: '1.0.0', enabled: true, official: false },
  ])
  await tick()

  try {
    watchdogState.budgets = { '@x/warn': { moduleLoadMB: 350, version: '1.0.0' }, '@x/throttle': { moduleLoadMB: 520, version: '1.0.0' } }
    await watchdog.checkMemoryTiers()
    const incidents = watchdog.snapshot().incidents
    assert.ok(incidents.some((incident) => incident.kind === 'plugin-memory' && incident.level === 'warn' && incident.target === '@x/warn'))
    assert.ok(incidents.some((incident) => incident.kind === 'plugin-memory' && incident.level === 'throttle' && incident.target === '@x/throttle'))
    assert.equal(watchdogState.memoryTiers.get('@x/throttle').tier, 'throttle')
    const denial = decideToolGuard({ name: 'plugin_manager', arguments: { action: 'install_bundle', target: '@x/throttle' } }, watchdogState)
    assert.ok(denial, 'additive operation denied at the throttle tier')
  } finally {
    watchdog.dispose()
    fs.rmSync(path.dirname(watchdogState.dataDir), { recursive: true, force: true })
  }
})

test('1000 MB force-closes and quarantines the plugin', async () => {
  const { watchdog, manager } = makeWatchdog([{ name: '@x/close', version: '1.0.0', enabled: true, official: false }])
  await tick()

  try {
    watchdogState.budgets = { '@x/close': { moduleLoadMB: 1100, version: '1.0.0' } }
    await watchdog.checkMemoryTiers()
    assert.ok(watchdogState.quarantine.has('@x/close'))
    assert.ok(manager.calls.some((call) => call.target === '@x/close' && call.enabled === false))
    assert.ok(watchdog.snapshot().incidents.some((incident) => incident.level === 'close'))
  } finally {
    watchdog.dispose()
    fs.rmSync(path.dirname(watchdogState.dataDir), { recursive: true, force: true })
  }
})

test('2000 MB force-uninstalls through pluginManager.removeBundle', async () => {
  const { watchdog, manager } = makeWatchdog([{ name: '@x/uninstall', version: '1.0.0', enabled: true, official: false }])
  await tick()

  try {
    watchdogState.budgets = { '@x/uninstall': { moduleLoadMB: 2100, version: '1.0.0' } }
    await watchdog.checkMemoryTiers()
    assert.deepEqual(manager.removed, ['@x/uninstall'])
    assert.ok(watchdog.snapshot().incidents.some((incident) => incident.level === 'uninstall' && /强制卸载/.test(incident.detail)))
  } finally {
    watchdog.dispose()
    fs.rmSync(path.dirname(watchdogState.dataDir), { recursive: true, force: true })
  }
})

test('3000 MB closes every third-party plugin but leaves official/installation plugins alone', async () => {
  const { watchdog, manager } = makeWatchdog([
    { name: '@twinsearth/x-huge', version: '1.0.0', enabled: true, official: false },
    { name: '@twinsearth/x-other', version: '1.0.0', enabled: true, official: false },
    { name: '@deepseek-ai/dsh-official-x', version: '1.0.0', enabled: true, official: true },
    { name: '@deepseek-ai/dsh-base', version: '1.0.0', enabled: true, official: true },
  ])
  await tick()

  try {
    watchdogState.budgets = { '@twinsearth/x-huge': { moduleLoadMB: 3100, version: '1.0.0' } }
    await watchdog.checkMemoryTiers()
    const closed = manager.calls.filter((call) => call.enabled === false).map((call) => call.target)
    assert.ok(closed.includes('@twinsearth/x-huge'))
    assert.ok(closed.includes('@twinsearth/x-other'))
    assert.ok(!closed.includes('@deepseek-ai/dsh-official-x'))
    assert.ok(!closed.includes('@deepseek-ai/dsh-base'))
  } finally {
    watchdog.dispose()
    fs.rmSync(path.dirname(watchdogState.dataDir), { recursive: true, force: true })
  }
})
