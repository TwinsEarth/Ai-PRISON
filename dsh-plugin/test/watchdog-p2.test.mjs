import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'
import { createWatchdog } from '../lib/watchdog-runtime.js'
import { resetWatchdogState } from '../lib/watchdog-state.js'
import { probeBudget } from '../lib/budget-probe.js'

const pluginRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')

function makeCtx(manager) {
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
      if (deps.includes('pluginManager') && manager) callback({ pluginManager: manager, effect: (fn) => fn() })
      if (deps.includes('tools')) callback({ tools: { guard: () => () => {} }, effect: (fn) => fn() })
    },
  }
  return { ctx, state }
}

function makeDirs() {
  const dir = fs.mkdtempSync(path.join(pluginRoot, '.tmp-p2-'))
  const logs = path.join(dir, 'logs')
  const journal = path.join(dir, 'journal')
  const state = path.join(dir, 'state')
  fs.mkdirSync(logs, { recursive: true })
  fs.mkdirSync(journal, { recursive: true })
  return { dir, logs, journal, state }
}

const CRASH = `time: 2026-01-01T00:00:00.000Z
source: host
phase: running

--- error ---
Error: dsh desktop host exited with 134: boom
  hostRunId: 'run-x'
FATAL ERROR: MarkCompactCollector: young object promotion failed Allocation failed - JavaScript heap out of memory`

test('enforce mode quarantines the crash attribution and writes pending-boot', async () => {
  resetWatchdogState()
  const dirs = makeDirs()
  fs.writeFileSync(path.join(dirs.logs, 'crash-2026-01-01T00-00-00-000Z-host.log'), CRASH)
  fs.writeFileSync(
    path.join(dirs.journal, 'operations.jsonl'),
    JSON.stringify({ runId: 'run-x', operation: { id: 'op-1', time: '2025-12-31T23:59:00.000Z', reason: 'install', target: '@a/bad', stage: 'install', enabled: true, phase: 'settled', application: 'applied', changed: true } }) + '\n',
  )
  const calls = []
  const manager = {
    async listBundles() {
      return [{ name: '@a/bad', version: '1.0.0', enabled: true }]
    },
    async setBundleEnabled(target, enabled) {
      calls.push({ target, enabled })
      return { application: 'applied', changed: true }
    },
  }
  const { ctx } = makeCtx(manager)
  const watchdog = createWatchdog(ctx, { dataDir: dirs.state, logsDir: dirs.logs, journalDir: dirs.journal, sampleMs: 60_000, crashFallback: 'enforce' }, { info() {}, warn() {} })
  try {
    await new Promise((resolve) => setTimeout(resolve, 80))
    const snapshot = watchdog.snapshot()
    assert.equal(snapshot.crashFallback, 'enforce')
    assert.ok(calls.some((call) => call.target === '@a/bad' && call.enabled === false), 'crash target disabled')
    assert.ok(snapshot.quarantine.some((entry) => entry.target === '@a/bad' && entry.reason === 'crash'))
    const pending = JSON.parse(fs.readFileSync(path.join(dirs.state, 'pending-boot.json'), 'utf8'))
    assert.equal(pending.level, 'L1')
    assert.deepEqual(pending.disable, ['@a/bad'])
    assert.ok(snapshot.incidents.some((entry) => entry.kind === 'crash-enforced'))
  } finally {
    watchdog.dispose()
    fs.rmSync(dirs.dir, { recursive: true, force: true })
  }
})

test('module-load budget probe measures a plugin package in isolation', async () => {
  const dir = fs.mkdtempSync(path.join(pluginRoot, '.tmp-budget-'))
  const pkgDir = path.join(dir, 'node_modules', '@probe', 'fixture')
  fs.mkdirSync(pkgDir, { recursive: true })
  fs.writeFileSync(path.join(dir, 'package.json'), JSON.stringify({ name: 'budget-host', private: true }))
  fs.writeFileSync(path.join(pkgDir, 'package.json'), JSON.stringify({ name: '@probe/fixture', version: '1.0.0', type: 'module', main: 'index.js' }))
  fs.writeFileSync(
    path.join(pkgDir, 'index.js'),
    'const rows = []\nfor (let i = 0; i < 120000; i += 1) rows.push({ i, text: "x".repeat(16) })\nglobalThis.__probeKeep = rows\nexport const ready = true\n',
  )
  const result = await probeBudget({ profileDir: dir, packageName: '@probe/fixture', timeoutMs: 20_000 })
  try {
    assert.equal(result.ok, true, JSON.stringify(result))
    assert.ok(result.moduleLoadMB >= 1, `moduleLoadMB=${result.moduleLoadMB}`)
    assert.ok(result.rssMB > 0)
  } finally {
    fs.rmSync(dir, { recursive: true, force: true })
  }
})
