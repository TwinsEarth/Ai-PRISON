import assert from 'node:assert/strict'
import test from 'node:test'
import { prunePluginState } from '../lib/watchdog-policy.js'
import { resetWatchdogState, watchdogState } from '../lib/watchdog-state.js'

test('pruning drops disabled targets, version-changed budgets and stale rankings/RSS', () => {
  resetWatchdogState()
  watchdogState.operations.set('@a/live', { count: 2, estimateMB: 5, lastAt: null })
  watchdogState.operations.set('@a/gone', { count: 9, estimateMB: 99, lastAt: null })
  watchdogState.budgets = {
    '@a/live': { moduleLoadMB: 10, version: '2.0.0' },
    '@a/gone': { moduleLoadMB: 99, version: '1.0.0' },
    '@a/upgraded': { moduleLoadMB: 20, version: '1.0.0' },
  }
  watchdogState.ranking = { '@a/live': { score: 10 }, '@a/gone': { score: 99 } }
  watchdogState.rss = { '@a/live': 1024, '@a/gone': 2048 }

  const rows = [
    { name: '@a/live', version: '2.0.0', enabled: true },
    { name: '@a/upgraded', version: '2.0.0', enabled: true },
    { name: '@a/gone', version: '1.0.0', enabled: false },
  ]
  const removed = prunePluginState(watchdogState, rows)

  assert.deepEqual(removed.operations, ['@a/gone'])
  assert.deepEqual(removed.budgets.sort(), ['@a/gone', '@a/upgraded'])
  assert.deepEqual(removed.ranking, ['@a/gone'])
  assert.deepEqual(removed.rss, ['@a/gone'])
  assert.ok(watchdogState.operations.has('@a/live'))
  assert.deepEqual(Object.keys(watchdogState.budgets), ['@a/live'])
  assert.deepEqual(Object.keys(watchdogState.ranking), ['@a/live'])
  assert.deepEqual(Object.keys(watchdogState.rss), ['@a/live'])
})

test('pruning a fully enabled, unchanged set removes nothing', () => {
  resetWatchdogState()
  watchdogState.operations.set('@a/live', { count: 1 })
  watchdogState.budgets = { '@a/live': { moduleLoadMB: 10, version: '1.0.0' } }
  watchdogState.ranking = { '@a/live': { score: 10 } }
  watchdogState.rss = { '@a/live': 512 }
  const removed = prunePluginState(watchdogState, [{ name: '@a/live', version: '1.0.0', enabled: true }])
  assert.deepEqual(removed, { operations: [], budgets: [], ranking: [], rss: [] })
  assert.equal(watchdogState.operations.size, 1)
})
