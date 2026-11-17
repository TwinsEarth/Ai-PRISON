import assert from 'node:assert/strict'
import test from 'node:test'
import { resolveSessionCwd } from '../lib/api.js'
import { summarizeRun } from '../lib/status.js'

function fakeCtx({ sessions, workingDirectory } = {}) {
  return {
    get(key) {
      if (key === 'sessions') return sessions
      if (key === 'workingDirectory') return workingDirectory
      return undefined
    },
  }
}

test('resolveSessionCwd prefers the effective directory of the newest live session', () => {
  const sessions = { list: () => [{ header: { cwd: 'C:/first' } }, { header: { cwd: 'C:/second' } }] }
  const workingDirectory = {
    get(session) {
      return session.header.cwd === 'C:/second' ? 'E:/workspace' : undefined
    },
  }
  assert.equal(resolveSessionCwd(fakeCtx({ sessions, workingDirectory })), 'E:/workspace')
})

test('resolveSessionCwd falls back to the session header cwd and then to undefined', () => {
  const sessions = { list: () => [{ header: { cwd: 'C:/only' } }] }
  assert.equal(resolveSessionCwd(fakeCtx({ sessions })), 'C:/only')
  assert.equal(resolveSessionCwd(fakeCtx({ sessions: { list: () => [] } })), undefined)
  assert.equal(resolveSessionCwd(undefined), undefined)
  assert.equal(resolveSessionCwd(fakeCtx({ sessions: { list: () => { throw new Error('boom') } } })), undefined)
})

test('summarizeRun surfaces the demo drill-relevant lines', () => {
  const demo = { ok: true, exitCode: 0, stdout: '── 落盘摘要 ──\n  已落盘记录数:  80\n  段数:          1', stderr: '' }
  assert.match(summarizeRun('demo', demo), /80/)
  const drill = { ok: true, exitCode: 0, stdout: '  OVERALL: PASS — 全部演练断言通过', stderr: '' }
  assert.match(summarizeRun('drill', drill), /PASS/)
  const failed = { ok: false, exitCode: 1, stdout: '', stderr: 'boom' }
  assert.match(summarizeRun('demo', failed), /失败/)
  const timedOut = { ok: false, exitCode: null, timedOut: true, stdout: '', stderr: '' }
  assert.match(summarizeRun('drill', timedOut), /超时/)
})
