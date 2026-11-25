import assert from 'node:assert/strict'
import fs from 'node:fs'
import path from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'
import { getHeapStatistics } from 'node:v8'
import { createWatchdog } from '../lib/watchdog-runtime.js'
import { MB, thresholdsFor } from '../lib/watchdog-policy.js'
import { resetWatchdogState, watchdogState } from '../lib/watchdog-state.js'

const pluginRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')

function makeWatchdog({ probe, rows, bundleVersion = '1.0.0' }) {
  resetWatchdogState()
  const dir = fs.mkdtempSync(path.join(pluginRoot, '.tmp-budget-'))
  const logs = path.join(dir, 'logs')
  const journal = path.join(dir, 'journal')
  const state = path.join(dir, 'state')
  fs.mkdirSync(logs, { recursive: true })
  fs.mkdirSync(journal, { recursive: true })
  fs.mkdirSync(state, { recursive: true })
  const calls = []
  const manager = {
    async listBundles() {
      return rows.map((name) => ({ name, version: bundleVersion, enabled: true }))
    },
    async setBundleEnabled(target, enabled) {
      calls.push({ target, enabled })
      return { application: 'applied', changed: true }
    },
  }
  const ctx = {
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
  const watchdog = createWatchdog(
    ctx,
    {
      profileDir: dir,
      dataDir: state,
      logsDir: logs,
      journalDir: journal,
      sampleMs: 60_000,
      tamperMonitor: false,
      probe,
      probeMax: 5,
      probeGapMs: 0,
    },
    { info() {}, warn() {} },
  )
  return { watchdog, calls, dir }
}

function cleanup(watchdog, dir) {
  watchdog.dispose()
  fs.rmSync(dir, { recursive: true, force: true })
}

test('a measured budget above 500 MB quarantines and closes the plugin', async () => {
  const { watchdog, calls, dir } = makeWatchdog({
    rows: ['@x/big'],
    probe: async () => ({ ok: true, moduleLoadMB: 600, rssMB: 40, ms: 12, entry: 'index.js' }),
  })
  try {
    const progress = await watchdog.probeNow({ force: true })
    assert.equal(progress.total, 1)
    const snapshot = watchdog.snapshot()
    const budget = snapshot.budgets['@x/big']
    assert.equal(budget.moduleLoadMB, 600)
    assert.equal(budget.version, '1.0.0')
    assert.equal(budget.source, 'module-load-probe')
    const jailed = snapshot.quarantine.find((entry) => entry.target === '@x/big')
    assert.ok(jailed && jailed.reason === 'budget')
    assert.ok(jailed.remediation && jailed.remediation.steps.length >= 2)
    assert.match(jailed.remediation.title, /内存/)
    assert.ok(calls.some((call) => call.target === '@x/big' && call.enabled === false))
    assert.ok(snapshot.incidents.some((incident) => incident.kind === 'plugin-budget' && incident.level === 'block'))
    const pending = JSON.parse(fs.readFileSync(path.join(dir, 'state', 'pending-boot.json'), 'utf8'))
    assert.deepEqual(pending.disable, ['@x/big'])
    const top = snapshot.topFive.find((row) => row.target === '@x/big')
    assert.ok(top)
    assert.equal(top.source, 'measured')
  } finally {
    cleanup(watchdog, dir)
  }
})

test('a budget between the warn and block lines only warns and stays measured in Top5', async () => {
  // The absolute lines are clamped to the real heap cap, so on a small-heap host
  // the block line sits well below the documented 500 MB. Derive a value strictly
  // between the two lines instead of hardcoding 420 MB.
  const { singleWarn, singleBlock } = thresholdsFor(getHeapStatistics().heap_size_limit)
  const betweenMB = Math.round((singleWarn + singleBlock) / 2 / MB)
  const { watchdog, calls, dir } = makeWatchdog({
    rows: ['@x/medium'],
    probe: async () => ({ ok: true, moduleLoadMB: betweenMB, rssMB: 20, ms: 8 }),
  })
  try {
    await watchdog.probeNow({ force: true })
    const snapshot = watchdog.snapshot()
    assert.equal(snapshot.budgets['@x/medium'].moduleLoadMB, betweenMB)
    assert.equal(snapshot.quarantine.length, 0)
    assert.equal(calls.length, 0)
    assert.ok(snapshot.incidents.some((incident) => incident.kind === 'plugin-budget' && incident.level === 'warn'))
    assert.equal(snapshot.topFive[0].source, 'measured')
    assert.equal(snapshot.topFive[0].estimateMB, betweenMB)
  } finally {
    cleanup(watchdog, dir)
  }
})

test('a failed probe is recorded with attempts and can be retried manually', async () => {
  let calls = 0
  const { watchdog, dir } = makeWatchdog({
    rows: ['@x/broken'],
    probe: async () => {
      calls += 1
      return { ok: false, error: 'probe-timeout' }
    },
  })
  try {
    await watchdog.probeNow({ force: true })
    let budget = watchdog.snapshot().budgets['@x/broken']
    assert.match(budget.error, /probe-timeout/)
    assert.equal(budget.attempts, 1)
    await watchdog.probeNow({ force: true })
    budget = watchdog.snapshot().budgets['@x/broken']
    assert.equal(budget.attempts, 2)
    assert.equal(calls, 2)
  } finally {
    cleanup(watchdog, dir)
  }
})

test('a thrown probe records the error and releases the running flag', async () => {
  let calls = 0
  const { watchdog, dir } = makeWatchdog({
    rows: ['@x/throwing'],
    probe: async () => {
      calls += 1
      throw new Error('boom')
    },
  })
  try {
    const first = await watchdog.probeNow({ force: true })
    assert.equal(first.total, 1)
    assert.equal(watchdogState.probeRunning, false)
    assert.match(watchdog.snapshot().budgets['@x/throwing'].error, /boom/)
    await watchdog.probeNow({ force: true })
    assert.equal(calls, 2, 'a second round must still be able to start')
  } finally {
    cleanup(watchdog, dir)
  }
})

test('two same-tick schedules start only one probe round', async () => {
  let calls = 0
  let release
  const { watchdog, dir } = makeWatchdog({
    rows: ['@x/slow'],
    probe: async () => {
      calls += 1
      await new Promise((resolve) => { release = resolve })
      return { ok: true, moduleLoadMB: 20, rssMB: 5, ms: 3 }
    },
  })
  try {
    const first = watchdog.probeNow({ force: true })
    const second = watchdog.probeNow({ force: true })
    await new Promise((resolve) => setTimeout(resolve, 20))
    assert.equal(calls, 1, 'second caller must not start another probe')
    release()
    await Promise.all([first, second])
  } finally {
    cleanup(watchdog, dir)
  }
})
