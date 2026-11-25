import assert from 'node:assert/strict'
import test from 'node:test'
import { REMEDIATION_REASONS, remediationFor } from '../lib/remediation.js'

test('every enforcement reason explains why, impact, steps and release', () => {
  for (const reason of REMEDIATION_REASONS) {
    const entry = remediationFor(reason)
    assert.equal(entry.code, reason)
    assert.ok(entry.title.length > 0, reason)
    assert.ok(entry.why.length > 10, reason)
    assert.ok(entry.impact.length > 0, reason)
    assert.ok(Array.isArray(entry.steps) && entry.steps.length >= 2, reason)
    assert.ok(entry.release.length > 0, reason)
  }
})

test('hot-ops remediation carries the observed count', () => {
  const entry = remediationFor('hot-ops', { count: 5.5 })
  assert.equal(entry.detail, '本次运行计数 5.5')
  assert.match(entry.steps.join(' '), /重启|假释/)
})

test('an unknown reason falls back to the hot-ops explanation', () => {
  const entry = remediationFor('does-not-exist')
  assert.equal(entry.code, 'hot-ops')
  assert.match(entry.title, /热操作/)
})

test('budget remediation states the pending-boot consumer boundary', () => {
  const entry = remediationFor('budget')
  assert.match(entry.impact, /不附带/)
  assert.match(entry.impact, /人工/)
  assert.doesNotMatch(entry.impact, /会被外部 guard 直接剔除/)
})
