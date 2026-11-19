import assert from 'node:assert/strict'
import test from 'node:test'
import { registerToolDefinition, resolveToolCwd } from '../lib/tools.js'

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

test('tools register through the official plain-definition path first', () => {
  const calls = []
  const tools = { register: (definition) => calls.push(definition) }
  const mode = registerToolDefinition(tools, { name: 'x' }, { defineTool: () => ({ wrapped: true }) })
  assert.equal(mode, 'plain')
  assert.deepEqual(calls, [{ name: 'x' }])
})

test('defineTool is used only when the plain registration fails', () => {
  const calls = []
  const tools = {
    register(definition) {
      if (!definition.wrapped) throw new Error('legacy shape')
      calls.push(definition)
    },
  }
  const mode = registerToolDefinition(tools, { name: 'x' }, { defineTool: (definition) => ({ wrapped: definition }) })
  assert.equal(mode, 'fallback')
  assert.deepEqual(calls, [{ wrapped: { name: 'x' } }])
})

test('a plain registration failure surfaces when no defineTool fallback exists', () => {
  const tools = { register() { throw new Error('boom') } }
  assert.throws(() => registerToolDefinition(tools, { name: 'x' }), /boom/)
})
