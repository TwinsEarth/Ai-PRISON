import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import test from 'node:test'
import { resolveDshDataDir, resolveJournalDir, resolveLogsDir } from '../lib/watchdog-core.js'
import { createWatchdog } from '../lib/watchdog-runtime.js'
import { resetWatchdogState } from '../lib/watchdog-state.js'

function withEnv(pairs, fn) {
  const saved = {}
  for (const key of Object.keys(pairs)) {
    saved[key] = process.env[key]
    if (pairs[key] === undefined) delete process.env[key]
    else process.env[key] = pairs[key]
  }
  try {
    return fn()
  } finally {
    for (const key of Object.keys(pairs)) {
      if (saved[key] === undefined) delete process.env[key]
      else process.env[key] = saved[key]
    }
  }
}

test('logs and journal honor explicit overrides', () => {
  withEnv({ DSH_LOG_DIR: '/tmp/dsh-logs-test', DSH_JOURNAL_DIR: '/tmp/dsh-journal-test' }, () => {
    assert.equal(resolveLogsDir(), '/tmp/dsh-logs-test')
    assert.equal(resolveJournalDir(), '/tmp/dsh-journal-test')
  })
})

test('the DSH data root follows the host platform default', () => {
  withEnv({ DSH_LOG_DIR: undefined, DSH_JOURNAL_DIR: undefined, XDG_DATA_HOME: undefined, XDG_CONFIG_HOME: undefined }, () => {
    const root = resolveDshDataDir()
    const logs = resolveLogsDir()
    if (process.platform === 'darwin') {
      assert.ok(root.endsWith(path.join('Library', 'Application Support', '@deepseek-ai', 'dsh-desktop')))
      assert.ok(logs.endsWith(path.join('Library', 'Logs', 'DeepSeek Harness')))
    } else if (process.platform === 'win32') {
      assert.ok(root.endsWith(path.join('@deepseek-ai', 'dsh-desktop')))
      assert.match(root, /AppData|Roaming/i)
      assert.ok(logs.endsWith(path.join('DeepSeek Harness', 'logs')))
    } else {
      assert.ok(root.endsWith(path.join('@deepseek-ai', 'dsh-desktop')))
      assert.ok(logs.endsWith(path.join('DeepSeek Harness', 'logs')))
      assert.match(logs, /config|XDG/)
    }
  })
})

test('platform-resolved crash and journal directories feed attribution end to end', async () => {
  resetWatchdogState()
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'ai-prison-paths-'))
  const logs = path.join(dir, 'logs')
  const journal = path.join(dir, 'journal')
  fs.mkdirSync(logs, { recursive: true })
  fs.mkdirSync(journal, { recursive: true })
  fs.writeFileSync(path.join(logs, 'crash-2026-01-01T00-00-01-000Z-host.log'), [
    'time: 2026-01-01T00:00:01.000Z',
    'source: host',
    'phase: running',
    '',
    '--- error ---',
    'Error: dsh desktop host exited with 134: boom',
    "  hostRunId: 'run-1'",
    'FATAL ERROR: MarkCompactCollector: young object promotion failed Allocation failed - JavaScript heap out of memory',
  ].join('\n'))
  fs.writeFileSync(path.join(journal, 'operations.jsonl'), JSON.stringify({
    runId: 'run-1',
    operation: { id: 'op-1', time: '2026-01-01T00:00:01.500Z', reason: 'bundle', target: '@x/crasher', stage: 'enable', enabled: true, version: '1.0.0', phase: 'started' },
  }) + '\n')
  const manager = {
    async listBundles() { return [{ name: '@x/crasher', version: '1.0.0', enabled: true }] },
    async setBundleEnabled() { return { application: 'applied', changed: true } },
  }
  const ctx = {
    effect: (fn) => { const disposer = fn(); return typeof disposer === 'function' ? disposer : () => {} },
    on: () => () => {},
    inject: (deps, callback) => {
      if (deps.includes('pluginManager')) callback({ pluginManager: manager, effect: (fn) => fn() })
      if (deps.includes('tools')) callback({ tools: { guard: () => () => {} }, effect: (fn) => fn() })
    },
  }
  try {
    await withEnv({ DSH_LOG_DIR: logs, DSH_JOURNAL_DIR: journal, DSH_PROFILE_DIR: dir }, async () => {
      const watchdog = createWatchdog(ctx, { dataDir: path.join(dir, 'state'), sampleMs: 60_000 }, { info() {}, warn() {} })
      try {
        await watchdog.readCrashLogs()
        await new Promise((resolve) => setTimeout(resolve, 60))
        const snapshot = watchdog.snapshot()
        const plan = snapshot.plans.at(-1)
        assert.ok(plan, 'crash plan exists')
        assert.equal(plan.target, '@x/crasher')
      } finally {
        watchdog.dispose()
      }
    })
  } finally {
    fs.rmSync(dir, { recursive: true, force: true })
  }
})
