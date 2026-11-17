import assert from 'node:assert/strict'
import test from 'node:test'
import {
  GB,
  MB,
  classifyWaterline,
  formatBytes,
  mergeEstimate,
  operationWeight,
  parseCrashLog,
  thresholdsFor,
  topOperations,
} from '../lib/watchdog-policy.js'

const six = 6 * GB
const four = 4 * GB

test('thresholds hit the designed values on a 6 GB heap', () => {
  const t = thresholdsFor(six)
  assert.equal(t.heapLimit, six)
  assert.equal(t.singleWarn, 300 * MB)
  assert.equal(t.singleBlock, 500 * MB)
  assert.equal(t.totalWarn, 3 * GB)
  assert.equal(t.totalPause, 3.9 * GB)
  assert.equal(t.totalBlock, 5 * GB)
  assert.equal(t.opWarn, 4 * GB)
  assert.equal(t.opBlock, 5 * GB)
})

test('thresholds stay below the cap on a 4 GB heap', () => {
  const t = thresholdsFor(four)
  assert.equal(t.singleWarn, 300 * MB)
  assert.equal(t.singleBlock, 500 * MB)
  assert.ok(t.totalWarn < four)
  assert.ok(t.totalPause < four)
  assert.ok(t.totalBlock < four)
  assert.ok(t.opBlock < four)
  assert.deepEqual([t.totalWarn, t.totalPause, t.totalBlock].map((v) => Math.round(v / (GB / 100)) / 100), [2.92, 3.52, 3.72])
})

test('classifyWaterline follows warn -> pause -> block with hysteresis-free boundaries', () => {
  const t = thresholdsFor(six)
  assert.equal(classifyWaterline(1 * GB, t), 'ok')
  assert.equal(classifyWaterline(t.totalWarn, t), 'warn')
  assert.equal(classifyWaterline(t.totalPause, t), 'pause')
  assert.equal(classifyWaterline(t.totalBlock, t), 'block')
  assert.equal(classifyWaterline(Number.NaN, t), 'ok')
  assert.equal(classifyWaterline(1 * GB, null), 'ok')
})

test('hot-operation weights match the design', () => {
  assert.equal(operationWeight({ stage: 'install', enabled: true }), 1)
  assert.equal(operationWeight({ stage: 'remove', enabled: false }), 1)
  assert.equal(operationWeight({ stage: 'enable', enabled: true }), 1)
  assert.equal(operationWeight({ stage: 'enable', enabled: false }), 0.5)
  assert.equal(operationWeight({ stage: 'enable' }), 1)
  assert.equal(operationWeight({ stage: 'unknown' }), 0)
  assert.equal(operationWeight(null), 0)
})

test('plugin heap estimates merge as a bounded weighted average', () => {
  assert.equal(mergeEstimate(0, 120), 120)
  assert.equal(mergeEstimate(100, 140), 120)
  assert.equal(mergeEstimate(100, -5), 100)
  assert.equal(mergeEstimate(Number.NaN, 80), 80)
})

test('parseCrashLog recognises a host OOM report', () => {
  const log = `time: 2026-11-17T21:18:58.034Z
source: host
phase: running
app: @deepseek-ai/dsh-desktop 0.2.2-rc.3

--- error ---
Error: dsh desktop host exited with 134: ...
FATAL ERROR: MarkCompactCollector: young object promotion failed Allocation failed - JavaScript heap out of memory`
  const parsed = parseCrashLog(log)
  assert.equal(parsed.valid, true)
  assert.equal(parsed.phase, 'running')
  assert.equal(parsed.hostExitCode, 134)
  assert.equal(parsed.oom, true)
  assert.match(parsed.time, /2026-11-17T21:18:58/)
})

test('parseCrashLog ignores unrelated text', () => {
  const parsed = parseCrashLog('hello world')
  assert.equal(parsed.valid, false)
  assert.equal(parsed.oom, false)
})

test('topOperations sorts by estimate then count and caps the list', () => {
  const operations = new Map([
    ['@a/one', { count: 2, estimateMB: 10 }],
    ['@b/two', { count: 1, estimateMB: 90 }],
    ['@c/three', { count: 6, estimateMB: 0 }],
  ])
  const rows = topOperations(operations, 2)
  assert.deepEqual(rows.map((row) => row.target), ['@b/two', '@a/one'])
  assert.equal(rows[0].estimateMB, 90)
  assert.equal(topOperations([], 5).length, 0)
})

test('formatBytes switches units', () => {
  assert.equal(formatBytes(300 * MB), '300 MB')
  assert.equal(formatBytes(6 * GB), '6.00 GB')
  assert.equal(formatBytes(-1), '—')
})
