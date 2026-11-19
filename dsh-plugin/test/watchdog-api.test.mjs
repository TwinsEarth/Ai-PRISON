import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import test from 'node:test'
import { registerWatchdogApi } from '../lib/watchdog-api.js'
import { resetWatchdogState, watchdogState } from '../lib/watchdog-state.js'

function makeCtx() {
  const routes = []
  return {
    routes,
    ctx: {
      inject: (deps, callback) => {
        if (!deps.includes('connection')) return
        callback({
          effect: (fn) => fn(),
          connection: { fetch: { register: (route) => { routes.push(route); return () => {} } } },
        })
      },
    },
  }
}

function makeActions() {
  const incidents = []
  return {
    incidents,
    appendIncident: (incident) => incidents.push(incident),
    scheduleProbe: async () => ({ done: 0, total: 0 }),
    releaseTarget: () => ({ ok: false }),
    disableTarget: async () => false,
  }
}

test('the heap6g request is written inside the watchdog state directory, not a machine path', async () => {
  resetWatchdogState()
  const dataDir = fs.mkdtempSync(path.join(os.tmpdir(), 'ai-prison-heap-'))
  watchdogState.dataDir = dataDir
  const { ctx, routes } = makeCtx()
  const actions = makeActions()
  try {
    registerWatchdogApi({ ctx, watchdogState, getSnapshot: () => ({ ok: true }), actions, logger: { warn() {} } })
    const actionRoute = routes.find((route) => route.path === '/api/ai-prison/watchdog/action')
    assert.ok(actionRoute)

    const enable = await actionRoute.fetch(new Request('http://localhost/api/ai-prison/watchdog/action', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ action: 'heap6g', enabled: true }),
    }))
    assert.equal(enable.status, 200)
    const enabledBody = await enable.json()
    assert.equal(enabledBody.enabled, true)
    assert.equal(enabledBody.consumer, 'external-launcher-required')
    assert.equal(path.dirname(enabledBody.requestFile), dataDir)
    assert.deepEqual(JSON.parse(fs.readFileSync(path.join(dataDir, 'heap6g-request.json'), 'utf8')).enabled, true)
    assert.ok(actions.incidents.some((incident) => incident.kind === 'heap6g'))

    const cancel = await actionRoute.fetch(new Request('http://localhost/api/ai-prison/watchdog/action', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ action: 'heap6g', enabled: false }),
    }))
    assert.equal((await cancel.json()).enabled, false)
    assert.equal(fs.existsSync(path.join(dataDir, 'heap6g-request.json')), false)
  } finally {
    fs.rmSync(dataDir, { recursive: true, force: true })
  }
})

test('unknown watchdog actions are rejected and status stays a plain GET', async () => {
  resetWatchdogState()
  const { ctx, routes } = makeCtx()
  try {
    registerWatchdogApi({ ctx, watchdogState, getSnapshot: () => ({ ok: true, mode: 'enforce' }), actions: makeActions(), logger: { warn() {} } })
    const actionRoute = routes.find((route) => route.path === '/api/ai-prison/watchdog/action')
    const statusRoute = routes.find((route) => route.path === '/api/ai-prison/watchdog/status')
    const unknown = await actionRoute.fetch(new Request('http://localhost/api/ai-prison/watchdog/action', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ action: 'self-destruct' }),
    }))
    assert.equal(unknown.status, 400)
    const status = await statusRoute.fetch(new Request('http://localhost/api/ai-prison/watchdog/status'))
    assert.equal(status.status, 200)
    assert.equal((await status.json()).mode, 'enforce')
  } finally {
    // stateless registration: nothing to clean up
  }
})
