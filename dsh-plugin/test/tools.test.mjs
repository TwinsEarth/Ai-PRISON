import assert from 'node:assert/strict'
import test from 'node:test'
import { resolveToolCwd } from '../lib/tools.js'

function fakeCtx({ sessions, workingDirectory } = {}) {
  return {
    get(key) {
      if (key === 'sessions') return sessions
      if (key === 'workingDirectory') return workingDirectory
      return undefined
    },
  }
}

test('resolveToolCwd maps the Agent id to a Session and prefers its effective directory', () => {
  const session = { header: { cwd: 'C:/original' } }
  const sessions = { get: (id) => (id === 'session-1' ? session : undefined) }
  const workingDirectory = { get: (s) => (s === session ? 'E:/workspace' : undefined) }
  const cwd = resolveToolCwd(fakeCtx({ sessions, workingDirectory }), { agent: { id: 'session-1' } })
  assert.equal(cwd, 'E:/workspace')
})

test('resolveToolCwd falls back to the session header cwd when the service rejects the session', () => {
  const session = { header: { cwd: 'E:/original' } }
  const sessions = { get: () => session }
  const workingDirectory = {
    get() {
      throw new Error('not a session')
    },
  }
  assert.equal(resolveToolCwd(fakeCtx({ sessions, workingDirectory }), { agent: { id: 's' } }), 'E:/original')
})

test('resolveToolCwd falls back to the Host default directory, then the process directory', () => {
  const ctxDefault = fakeCtx({ workingDirectory: { defaultDirectory: 'C:/launch' } })
  assert.equal(resolveToolCwd(ctxDefault, { agent: { id: 'missing' } }), 'C:/launch')
  assert.equal(resolveToolCwd(fakeCtx({}), { agent: { id: 'missing' } }), process.cwd())
  assert.equal(resolveToolCwd(undefined, undefined), process.cwd())
})
