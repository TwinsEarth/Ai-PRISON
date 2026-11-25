import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import test from 'node:test'
import { createEnforcement } from '../lib/watchdog-enforcement.js'
import { resetWatchdogState, watchdogState } from '../lib/watchdog-state.js'

function makeEnforcement() {
  const incidents = []
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'ai-prison-alert-'))
  const enforcement = createEnforcement({
    watchdogState,
    refs: { manager: null },
    dataDir: dir,
    journalDir: dir,
    exemptions: new Set(),
    appendIncident: (incident) => incidents.push(incident),
    persist: () => {},
  })
  return { enforcement, incidents, dir }
}

test('alert policy records a notification and a distinct incident; observe does not', () => {
  resetWatchdogState()
  const { enforcement, incidents, dir } = makeEnforcement()
  try {
    watchdogState.enforce = true
    watchdogState.tamper = { policy: 'alert' }
    enforcement.handleTamper({ diffs: [{ file: 'a.yml' }], restored: false })
    const alert = incidents.find((incident) => incident.kind === 'tamper')
    assert.equal(alert.notify, true)
    assert.equal(alert.policy, 'alert')
    assert.match(alert.detail, /发出告警事件/)

    incidents.length = 0
    watchdogState.tamper = { policy: 'observe' }
    enforcement.handleTamper({ diffs: [{ file: 'a.yml' }], restored: false })
    const observe = incidents.find((incident) => incident.kind === 'tamper')
    assert.equal(observe.notify, false)
    assert.match(observe.detail, /仅记录/)
  } finally {
    fs.rmSync(dir, { recursive: true, force: true })
  }
})
