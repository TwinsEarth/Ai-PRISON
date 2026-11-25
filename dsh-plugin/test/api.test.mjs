import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import test from 'node:test'
import { registerApi, resetRunThrottle, resolveAllowedDataDir, resolveSessionCwd } from '../lib/api.js'
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

function tempCwd() {
  return fs.mkdtempSync(path.join(os.tmpdir(), 'ai-prison-api-'))
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

test('HTTP data_dir defaults to the configured or working-directory ledger', () => {
  const cwd = tempCwd()
  try {
    const fallback = resolveAllowedDataDir({ cwd })
    assert.equal(fallback.ok, true)
    assert.equal(fallback.dir, path.join(cwd, '.ai-prison'))
    const relative = resolveAllowedDataDir({ requested: '.ai-prison', cwd })
    assert.equal(relative.ok, true)
    assert.equal(relative.dir, path.join(cwd, '.ai-prison'))
  } finally {
    fs.rmSync(cwd, { recursive: true, force: true })
  }
})

test('HTTP data_dir may name the configured ledger directory', () => {
  const cwd = tempCwd()
  const configured = path.join(cwd, 'custom-ledger')
  try {
    const allowed = resolveAllowedDataDir({ requested: configured, config: { dataDir: configured }, cwd })
    assert.equal(allowed.ok, true)
    assert.equal(allowed.dir, configured)
  } finally {
    fs.rmSync(cwd, { recursive: true, force: true })
  }
})

test('HTTP data_dir rejects arbitrary directories and path escapes', () => {
  const cwd = tempCwd()
  const outside = path.join(path.dirname(cwd), 'somewhere-else', '.ai-prison')
  try {
    for (const requested of [outside, path.join(cwd, 'other'), 'C:/Windows/System32', '\\\\server\\share']) {
      const result = resolveAllowedDataDir({ requested, cwd })
      assert.equal(result.ok, false, `${requested} must be rejected`)
      assert.equal(result.reason, 'not-allowed')
    }
    const escape = resolveAllowedDataDir({ requested: path.join(cwd, '..', path.basename(cwd), '.ai-prison'), cwd })
    assert.equal(escape.ok, true, 'resolving back into the allowed directory is fine')
    assert.equal(resolveAllowedDataDir({ requested: '', cwd }).reason, 'invalid')
    assert.equal(resolveAllowedDataDir({ requested: 42, cwd }).reason, 'invalid')
  } finally {
    fs.rmSync(cwd, { recursive: true, force: true })
  }
})

test('the status and run routes answer 403 before touching the CLI for a foreign data_dir', async () => {
  const routes = []
  const ctx = {
    get: () => undefined,
    inject: (deps, callback) => {
      if (!deps.includes('connection')) return
      callback({
        effect: (fn) => fn(),
        connection: {
          fetch: {
            register: (route) => {
              routes.push(route)
              return () => {}
            },
          },
        },
      })
    },
  }
  registerApi({ ctx, config: {}, logger: { warn() {} } })
  assert.equal(routes.length, 2, 'status and run routes are registered')

  const statusRoute = routes.find((route) => route.path === '/api/ai-prison/status')
  const runRoute = routes.find((route) => route.path === '/api/ai-prison/run')
  const outside = path.join(os.tmpdir(), 'ai-prison-foreign', '.ai-prison')

  const statusResponse = await statusRoute.fetch(new Request(`http://localhost/api/ai-prison/status?data_dir=${encodeURIComponent(outside)}`))
  assert.equal(statusResponse.status, 403)
  const statusBody = await statusResponse.json()
  assert.equal(statusBody.reason, 'data-dir-not-allowed')
  assert.ok(Array.isArray(statusBody.allowed))

  const runResponse = await runRoute.fetch(new Request('http://localhost/api/ai-prison/run', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ action: 'reset', data_dir: outside }),
  }))
  assert.equal(runResponse.status, 403)
  const runBody = await runResponse.json()
  assert.equal(runBody.reason, 'data-dir-not-allowed')
})

test('POST /run is rate limited between two rapid executions', async () => {
  const cwd = tempCwd()
  resetRunThrottle()
  const routes = []
  const ctx = {
    get(key) {
      if (key === 'sessions') return { list: () => [{ header: { cwd } }] }
      return undefined
    },
    inject: (deps, callback) => {
      if (!deps.includes('connection')) return
      callback({
        effect: (fn) => fn(),
        connection: { fetch: { register: (route) => { routes.push(route); return () => {} } } },
      })
    },
  }
  registerApi({ ctx, config: {}, logger: { warn() {} } })
  const runRoute = routes.find((route) => route.path === '/api/ai-prison/run')
  const request = () => new Request('http://localhost/api/ai-prison/run', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ action: 'reset' }),
  })
  try {
    const first = await runRoute.fetch(request())
    assert.equal(first.status, 200)
    const second = await runRoute.fetch(request())
    assert.equal(second.status, 429)
    const payload = await second.json()
    assert.equal(payload.reason, 'rate-limited')
    assert.ok(payload.retryAfterMs > 0)
  } finally {
    resetRunThrottle()
    fs.rmSync(cwd, { recursive: true, force: true })
  }
})

test('POST reset-demo probes the CLI before moving any ledger segment', async () => {
  const cwd = tempCwd()
  const dataDir = path.join(cwd, '.ai-prison')
  fs.mkdirSync(dataDir, { recursive: true })
  const segment = path.join(dataDir, 'seg-0000000000.log')
  fs.writeFileSync(segment, 'keep-me')
  resetRunThrottle()
  const routes = []
  const ctx = {
    get(key) {
      if (key === 'sessions') return { list: () => [{ header: { cwd } }] }
      return undefined
    },
    inject: (deps, callback) => {
      if (!deps.includes('connection')) return
      callback({ effect: (fn) => fn(), connection: { fetch: { register: (route) => { routes.push(route); return () => {} } } } })
    },
  }
  try {
    registerApi({ ctx, config: { cliPath: path.join(cwd, 'missing-aiprison') }, logger: { warn() {} } })
    const runRoute = routes.find((route) => route.path === '/api/ai-prison/run')
    const response = await runRoute.fetch(new Request('http://localhost/api/ai-prison/run', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ action: 'reset-demo' }),
    }))
    assert.equal(response.status, 409)
    const payload = await response.json()
    assert.equal(payload.reason, 'cli-missing')
    assert.equal(fs.readFileSync(segment, 'utf8'), 'keep-me', 'segment must survive a missing CLI')
    assert.equal(fs.existsSync(path.join(dataDir, '.trash')), false, 'no trash is created before the CLI check')
  } finally {
    resetRunThrottle()
    fs.rmSync(cwd, { recursive: true, force: true })
  }
})

test('the unknown-action error enumerates every accepted action', async () => {
  const routes = []
  const ctx = {
    get: () => undefined,
    inject: (deps, callback) => {
      if (!deps.includes('connection')) return
      callback({ effect: (fn) => fn(), connection: { fetch: { register: (route) => { routes.push(route); return () => {} } } } })
    },
  }
  registerApi({ ctx, config: {}, logger: { warn() {} } })
  const runRoute = routes.find((route) => route.path === '/api/ai-prison/run')
  const response = await runRoute.fetch(new Request('http://localhost/api/ai-prison/run', {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ action: 'not-an-action' }),
  }))
  assert.equal(response.status, 400)
  const payload = await response.json()
  assert.match(payload.error, /reset-drill/)
  assert.match(payload.error, /reset-demo/)
})
