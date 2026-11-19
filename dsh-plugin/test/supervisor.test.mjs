import test from 'node:test'
import assert from 'node:assert/strict'
import { SUPERVISOR_DEFAULTS, createSupervisorState, classifyExit, describeVerdict, evaluateSample, supervisorThresholds } from '../lib/supervisor.js'

const GB = 1024 * 1024 * 1024
const MB = 1024 * 1024
const T0 = 1_700_000_000_000

test('thresholds follow the host heap cap on both 4 GB and 6 GB hosts', () => {
  const four = supervisorThresholds(4 * GB)
  assert.equal(four.heapLimitMB, 4096)
  assert.equal(four.warnMB, 2990)
  assert.equal(four.killMB, 3809)
  const six = supervisorThresholds(6 * GB)
  assert.equal(six.heapLimitMB, 6144)
  assert.equal(six.warnMB, 4485)
  assert.equal(six.killMB, 5714)
  assert.equal(four.strikeLimit, SUPERVISOR_DEFAULTS.strikeLimit)
})

test('a sample under the warn line stays ok and clears strikes', () => {
  const state = createSupervisorState(supervisorThresholds(4 * GB))
  const d = evaluateSample(state, { rssBytes: 1200 * MB }, T0)
  assert.equal(d.action, 'ok')
  assert.equal(state.verdict, 'ok')
  assert.equal(state.strikes, 0)
})

test('crossing the warn line alone only warns, never escalates', () => {
  const state = createSupervisorState(supervisorThresholds(4 * GB))
  const d = evaluateSample(state, { rssBytes: 3100 * MB }, T0)
  assert.equal(d.action, 'warn')
  assert.equal(state.verdict, 'warn')
  assert.equal(state.strikes, 0)
  assert.match(d.reason, /warn line 2990 MB/)
})

test('three consecutive kill-line samples escalate to one request-exit, then kill after grace', () => {
  const state = createSupervisorState(supervisorThresholds(4 * GB, { graceMs: 5000 }))
  const rss = { rssBytes: 3900 * MB }
  assert.equal(evaluateSample(state, rss, T0).action, 'warn')
  assert.equal(state.strikes, 1)
  assert.equal(evaluateSample(state, rss, T0 + 1000).action, 'warn')
  assert.equal(state.strikes, 2)
  const third = evaluateSample(state, rss, T0 + 2000)
  assert.equal(third.action, 'request-exit')
  assert.match(third.reason, /kill line 3809 MB/)
  assert.equal(state.verdict, 'request-exit')
  assert.equal(evaluateSample(state, rss, T0 + 3000).action, 'ok')
  const late = evaluateSample(state, rss, T0 + 8000)
  assert.equal(late.action, 'kill')
  assert.equal(state.verdict, 'kill')
})

test('a rapid rise triggers request-exit before the absolute kill line', () => {
  const state = createSupervisorState(supervisorThresholds(6 * GB))
  assert.equal(evaluateSample(state, { rssBytes: 900 * MB }, T0).action, 'ok')
  assert.equal(evaluateSample(state, { rssBytes: 1100 * MB }, T0 + 1000).rate, 200)
  const second = evaluateSample(state, { rssBytes: 1300 * MB }, T0 + 2000)
  assert.equal(second.action, 'request-exit')
  assert.match(second.reason, /growth 200 MB\/s/)
})

test('a flat curve never accumulates rate strikes', () => {
  const state = createSupervisorState(supervisorThresholds(4 * GB))
  for (let i = 0; i < 5; i += 1) evaluateSample(state, { rssBytes: 1500 * MB }, T0 + i * 1000)
  assert.equal(state.rateStrikes, 0)
  assert.equal(state.verdict, 'ok')
})

test('classifyExit separates clean, startup, sync-blocked, heap-oom and exit-1 deaths', () => {
  const base = () => createSupervisorState(supervisorThresholds(4 * GB))
  const clean = base()
  evaluateSample(clean, { rssBytes: 2000 * MB }, T0)
  assert.equal(classifyExit(clean, { exitCode: 0 }), 'clean-exit')

  const startup = base()
  assert.equal(classifyExit(startup, { exitCode: 134 }), 'startup-died-before-sampling')

  const blocked = base()
  evaluateSample(blocked, { rssBytes: 3200 * MB }, T0)
  assert.equal(classifyExit(blocked, { exitCode: 134, gapMs: 4000 }), 'sync-blocked')

  const oom = base()
  evaluateSample(oom, { rssBytes: 3500 * MB }, T0)
  assert.equal(classifyExit(oom, { exitCode: 134, gapMs: 900 }), 'heap-oom')

  const one = base()
  evaluateSample(one, { rssBytes: 800 * MB }, T0)
  assert.equal(classifyExit(one, { exitCode: 1 }), 'exit-1')

  const other = base()
  evaluateSample(other, { rssBytes: 800 * MB }, T0)
  assert.equal(classifyExit(other, { exitCode: 5, signal: 'SIGSEGV' }), 'abnormal-exit')
})

test('describeVerdict carries the numbers an incident needs', () => {
  const state = createSupervisorState(supervisorThresholds(4 * GB))
  evaluateSample(state, { rssBytes: 3400 * MB }, T0)
  const line = describeVerdict(state)
  assert.match(line, /verdict=warn/)
  assert.match(line, /peak=3400MB/)
  assert.match(line, /warn=2990MB/)
  assert.match(line, /reason=rss 3400 MB >= warn line 2990 MB/)
})
