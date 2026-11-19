import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import test from 'node:test'
import { createPersistence } from '../lib/watchdog-persistence.js'
import { resetWatchdogState, watchdogState } from '../lib/watchdog-state.js'

function makeStore() {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'ai-prison-persist-'))
  return {
    dir,
    stateFile: path.join(dir, 'state.json'),
    incidentsFile: path.join(dir, 'incidents.jsonl'),
  }
}

test('state.json is replaced atomically and restores the saved snapshot', () => {
  resetWatchdogState()
  const store = makeStore()
  try {
    watchdogState.startedAt = new Date().toISOString()
    watchdogState.baseline = { at: '2026-01-01T00:00:00.000Z', runId: 'run-1', bundles: [{ name: '@a/b', version: '1.0.0', enabled: true }] }
    watchdogState.budgets = { '@a/b': { moduleLoadMB: 12, version: '1.0.0' } }
    watchdogState.quarantine.set('@a/b', { reason: 'crash', level: 'L1', release: 'manual' })
    const persistence = createPersistence({ watchdogState, stateFile: store.stateFile, incidentsFile: store.incidentsFile })
    persistence.persist(true)
    assert.ok(fs.existsSync(store.stateFile))
    assert.equal(fs.existsSync(`${store.stateFile}.tmp`), false)
    const saved = JSON.parse(fs.readFileSync(store.stateFile, 'utf8'))
    assert.equal(saved.baseline.runId, 'run-1')
    assert.equal(saved.quarantine[0].target, '@a/b')

    resetWatchdogState()
    const restored = createPersistence({ watchdogState, stateFile: store.stateFile, incidentsFile: store.incidentsFile })
    restored.restoreState()
    assert.equal(watchdogState.baseline.runId, 'run-1')
    assert.equal(watchdogState.budgets['@a/b'].moduleLoadMB, 12)
    assert.ok(watchdogState.quarantine.has('@a/b'))
  } finally {
    fs.rmSync(store.dir, { recursive: true, force: true })
  }
})

test('an oversized incidents file is rotated to the last 2000 lines atomically', () => {
  resetWatchdogState()
  const store = makeStore()
  try {
    const longLine = JSON.stringify({ at: '2026-01-01T00:00:00.000Z', kind: 'noise', detail: 'x'.repeat(380) })
    fs.writeFileSync(store.incidentsFile, Array.from({ length: 3000 }, () => longLine).join('\n') + '\n')
    assert.ok(fs.statSync(store.incidentsFile).size > 1024 * 1024)
    const persistence = createPersistence({ watchdogState, stateFile: store.stateFile, incidentsFile: store.incidentsFile })
    persistence.appendIncident({ kind: 'test', detail: 'after-rotation' })
    const lines = fs.readFileSync(store.incidentsFile, 'utf8').trim().split('\n')
    assert.equal(lines.length, 2000, 'rotation keeps the last 2000 lines')
    assert.match(lines.at(-1), /after-rotation/)
    assert.equal(fs.existsSync(`${store.incidentsFile}.tmp`), false)
  } finally {
    fs.rmSync(store.dir, { recursive: true, force: true })
  }
})
