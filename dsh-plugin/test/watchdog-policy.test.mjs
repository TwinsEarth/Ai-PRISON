import assert from 'node:assert/strict'
import test from 'node:test'
import {
  FREEZE_MARGIN,
  GB,
  MB,
  allowedDuringFreeze,
  attributeCrash,
  classifyWaterline,
  decideToolGuard,
  diffBaseline,
  escalationLevel,
  formatBytes,
  hotOperationTriggered,
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
  assert.deepEqual(
    [t.totalWarn, t.totalPause, t.totalBlock, t.opWarn, t.opBlock].map((value) => Math.round(value / (GB / 100)) / 100),
    [2.92, 3.52, 3.72, 2.67, 3.33],
  )
})

test('waterline follows ok -> warn -> pause -> frost with hysteresis', () => {
  const t = thresholdsFor(six)
  assert.equal(classifyWaterline(1 * GB, t), 'ok')
  assert.equal(classifyWaterline(t.totalWarn, t), 'warn')
  assert.equal(classifyWaterline(t.totalPause, t), 'pause')
  assert.equal(classifyWaterline(t.totalBlock, t), 'frost')
  assert.equal(classifyWaterline(t.totalBlock - FREEZE_MARGIN / 2, t, 'frost'), 'frost')
  assert.equal(classifyWaterline(t.totalPause - FREEZE_MARGIN / 2, t, 'pause'), 'pause')
  assert.equal(classifyWaterline(t.totalWarn - FREEZE_MARGIN - 1, t), 'ok')
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

test('hot-operation trigger is strictly greater than five', () => {
  assert.equal(hotOperationTriggered(5), false)
  assert.equal(hotOperationTriggered(5.5), true)
  assert.equal(hotOperationTriggered(6), true)
})

test('tool guard blocks additive actions while paused or frozen', () => {
  const pausedState = { waterline: 'pause', graceUntil: 0, thresholds: thresholdsFor(six), quarantine: new Map() }
  const frozenState = { waterline: 'frost', graceUntil: 0, thresholds: thresholdsFor(six), quarantine: new Map() }
  assert.equal(decideToolGuard({ name: 'plugin_manager', arguments: { action: 'list_bundles' } }, frozenState), undefined)
  assert.ok(decideToolGuard({ name: 'plugin_manager', arguments: { action: 'set_bundle', target: '@x/y' } }, frozenState))
  assert.ok(decideToolGuard({ name: 'plugin_manager', arguments: { action: 'install_bundle', target: '@x/y' } }, pausedState))
  const graced = { ...pausedState, graceUntil: Date.now() + 60_000 }
  assert.equal(decideToolGuard({ name: 'plugin_manager', arguments: { action: 'set_bundle', target: '@x/y' } }, graced), undefined)
  assert.equal(decideToolGuard({ name: 'other_tool', arguments: {} }, frozenState), undefined)
  assert.equal(allowedDuringFreeze('close'), false)
})

test('tool guard refuses quarantined targets', () => {
  const state = {
    waterline: 'ok',
    graceUntil: 0,
    thresholds: thresholdsFor(six),
    quarantine: new Map([['@x/y', { reason: 'hot-ops', level: 'L2' }]]),
  }
  const denial = decideToolGuard({ name: 'plugin_manager', arguments: { action: 'set_bundle', target: '@x/y', enabled: true } }, state)
  assert.ok(denial)
  assert.match(denial.reason, /AI 监狱/)
  assert.equal(decideToolGuard({ name: 'plugin_manager', arguments: { action: 'list_plugins' } }, state), undefined)
})

test('plugin heap estimates merge as a bounded weighted average', () => {
  assert.equal(mergeEstimate(0, 120), 120)
  assert.equal(mergeEstimate(100, 140), 120)
  assert.equal(mergeEstimate(100, -5), 100)
  assert.equal(mergeEstimate(Number.NaN, 80), 80)
})

test('parseCrashLog recognises a host OOM report and its hostRunId', () => {
  const log = `time: 2026-11-17T21:18:58.034Z
source: host
phase: running

--- error ---
Error: dsh desktop host exited with 134: boom
  hostRunId: '24f0ce3e-5576-47fa-8125-93844a4f04e4'
FATAL ERROR: MarkCompactCollector: young object promotion failed Allocation failed - JavaScript heap out of memory`
  const parsed = parseCrashLog(log)
  assert.equal(parsed.valid, true)
  assert.equal(parsed.phase, 'running')
  assert.equal(parsed.hostExitCode, 134)
  assert.equal(parsed.oom, true)
  assert.equal(parsed.hostRunId, '24f0ce3e-5576-47fa-8125-93844a4f04e4')
})

test('attributeCrash prefers an in-flight operation in the crashed run', () => {
  const records = [
    { runId: 'run-a', operation: { id: '1', time: '2026-11-17T21:13:42.785Z', reason: 'bundle', target: 'dsh-context', stage: 'enable', enabled: true, version: '0.85.1', phase: 'started' } },
    { runId: 'run-a', operation: { id: '1', time: '2026-11-17T21:13:43.126Z', reason: 'bundle', target: 'dsh-context', stage: 'enable', enabled: true, version: '0.85.1', phase: 'settled', application: 'applied', changed: true } },
    { runId: 'run-a', operation: { id: '2', time: '2026-11-17T21:17:02.644Z', reason: 'bundle', target: '@twinsearth/dsh-w2m', stage: 'enable', enabled: true, version: '3.5.0-rc.1', phase: 'started' } },
    { runId: 'run-b', operation: { id: '3', time: '2026-11-17T21:18:00.000Z', reason: 'install', target: '@other/plugin', stage: 'install', enabled: true, phase: 'settled', application: 'applied', changed: true } },
    { runId: 'run-a', operation: { id: '4', time: '2026-11-17T21:16:00.000Z', reason: 'bundle', target: 'some-disabled', stage: 'enable', enabled: false, phase: 'settled', application: 'applied', changed: true } },
  ]
  const attribution = attributeCrash(records, { hostRunId: 'run-a' })
  assert.equal(attribution.picked.target, '@twinsearth/dsh-w2m')
  assert.equal(attribution.picked.inFlight, true)
  assert.deepEqual(attribution.candidates.map((candidate) => candidate.target), ['@twinsearth/dsh-w2m', 'dsh-context'])
})

test('attributeCrash falls back to the latest settled install, then enable', () => {
  const records = [
    { runId: 'run-a', operation: { id: '1', time: '2026-11-17T20:00:00.000Z', reason: 'install', target: '@a/install', stage: 'install', enabled: true, phase: 'settled', application: 'applied', changed: true } },
    { runId: 'run-a', operation: { id: '2', time: '2026-11-17T21:00:00.000Z', reason: 'bundle', target: '@b/enable', stage: 'enable', enabled: true, phase: 'settled', application: 'applied', changed: true } },
  ]
  const attribution = attributeCrash(records, { hostRunId: 'run-a' })
  assert.equal(attribution.picked.target, '@a/install')
  assert.equal(attribution.picked.inFlight, false)
})

test('attributeCrash returns no candidate when the run is missing', () => {
  const attribution = attributeCrash([], { hostRunId: 'run-a' })
  assert.equal(attribution.picked, null)
  assert.match(attribution.reason, /no candidate/)
})

test('diffBaseline reports added and updated enabled bundles only', () => {
  const baseline = { bundles: [{ name: 'a', version: '1.0.0', enabled: true }, { name: 'c', version: '9', enabled: true }] }
  const rows = [
    { name: 'a', version: '2.0.0', enabled: true },
    { name: 'b', version: '1.0.0', enabled: true },
    { name: 'c', version: '9', enabled: false },
  ]
  const diff = diffBaseline(rows, baseline)
  assert.deepEqual(diff.changed.map((row) => [row.name, row.kind]), [['a', 'updated'], ['b', 'added']])
  assert.equal(diffBaseline(rows, null).unknown, true)
})

test('escalationLevel maps crash counts to L1-L3', () => {
  assert.equal(escalationLevel(0), 'L0')
  assert.equal(escalationLevel(1), 'L1')
  assert.equal(escalationLevel(2), 'L2')
  assert.equal(escalationLevel(3), 'L3')
  assert.equal(escalationLevel(9), 'L3')
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
