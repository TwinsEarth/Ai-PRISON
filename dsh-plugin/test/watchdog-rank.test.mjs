import assert from 'node:assert/strict'
import test from 'node:test'
import { rankTopPlugins } from '../lib/watchdog-policy.js'

const MB = 1024 * 1024

test('child process RSS participates in the Top5 ranking', () => {
  const { rows } = rankTopPlugins({
    operations: [],
    budgets: { '@a/small': { moduleLoadMB: 10, version: '1' }, '@b/big': { moduleLoadMB: 5, version: '1' } },
    rss: { '@a/small': 10 * MB, '@b/big': 200 * MB },
    previous: {},
    limit: 5,
  })
  assert.equal(rows[0].target, '@b/big')
  assert.equal(rows[0].rssMB, 200)
  assert.equal(rows[0].score, 205)
  assert.equal(rows[1].target, '@a/small')
})

test('RSS is smoothed with EWMA and keeps the previous order while stable', () => {
  const first = rankTopPlugins({
    operations: [],
    budgets: { '@a/x': { moduleLoadMB: 100, version: '1' } },
    rss: { '@a/x': 100 * MB },
    previous: {},
    limit: 5,
  })
  assert.equal(first.rows[0].rssMB, 100)
  const smoothed = rankTopPlugins({
    operations: [],
    budgets: { '@a/x': { moduleLoadMB: 105, version: '1' } },
    rss: { '@a/x': 200 * MB },
    previous: first.nextRanking,
    limit: 5,
  })
  // 100 * 0.65 + 200 * 0.35 = 135
  assert.equal(smoothed.rows[0].rssMB, 135)
})

test('rank hysteresis keeps the old order under a small score change and reorders on a big one', () => {
  const previous = { '@a/x': { score: 100, rssMB: 0 }, '@b/y': { score: 90, rssMB: 0 } }
  const stable = rankTopPlugins({
    operations: [
      { target: '@a/x', estimateMB: 100, count: 0 },
      { target: '@b/y', estimateMB: 95, count: 0 },
    ],
    budgets: {},
    rss: {},
    previous,
    limit: 5,
  })
  assert.deepEqual(stable.rows.map((row) => row.target), ['@a/x', '@b/y'])
  const changed = rankTopPlugins({
    operations: [
      { target: '@a/x', estimateMB: 50, count: 0 },
      { target: '@b/y', estimateMB: 200, count: 0 },
    ],
    budgets: {},
    rss: {},
    previous,
    limit: 5,
  })
  assert.deepEqual(changed.rows.map((row) => row.target), ['@b/y', '@a/x'])
})
