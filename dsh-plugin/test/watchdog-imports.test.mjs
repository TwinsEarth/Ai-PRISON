import assert from 'node:assert/strict'
import fs from 'node:fs'
import path from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const runtimeFile = path.join(root, 'lib', 'watchdog-runtime.js')
const source = fs.readFileSync(runtimeFile, 'utf8')

test('watchdog-runtime imports every module it wires and no dead legacy scanner remains', () => {
  const modules = [
    './watchdog-core.js',
    './watchdog-persistence.js',
    './watchdog-enforcement.js',
    './watchdog-crash.js',
    './watchdog-budget.js',
    './watchdog-sampler.js',
    './watchdog-api.js',
    './watchdog-snapshot.js',
    './watchdog-state.js',
    './watchdog-policy.js',
    './boot-watch.js',
    './tamper-monitor.js',
    './rss-scan.js',
  ]
  for (const specifier of modules) {
    assert.ok(source.includes(`from '${specifier}'`), `runtime must import ${specifier}`)
    assert.ok(fs.existsSync(path.join(path.dirname(runtimeFile), specifier)), `${specifier} exists`)
  }
  // The 1.5.x direct-spawn scanner was replaced by rss-scan.js; its legacy
  // functions and the duplicated parser must never come back.
  assert.doesNotMatch(source, /function scanChildRss\b/, 'legacy scanChildRss stays removed')
  assert.doesNotMatch(source, /function startRssScanner\b/, 'legacy startRssScanner stays removed')
  assert.doesNotMatch(source, /RSS_QUERY_SCRIPT/, 'the runtime delegates RSS parsing to rss-scan.js')
})

test('each extracted watchdog module keeps a single responsibility', () => {
  const expected = {
    'watchdog-core.js': ['EXEMPT_TARGETS', 'readJournalRecords', 'resolveProfileDir'],
    'watchdog-persistence.js': ['createPersistence'],
    'watchdog-sampler.js': ['createSampler'],
    'watchdog-enforcement.js': ['createEnforcement'],
    'watchdog-crash.js': ['createCrash'],
    'watchdog-budget.js': ['createBudget'],
    'watchdog-api.js': ['registerWatchdogApi'],
    'watchdog-snapshot.js': ['watchdogSnapshot'],
  }
  for (const [file, markers] of Object.entries(expected)) {
    const text = fs.readFileSync(path.join(root, 'lib', file), 'utf8')
    for (const marker of markers) assert.ok(text.includes(marker), `${file} exports ${marker}`)
  }
  const runtimeLines = source.split('\n').length
  assert.ok(runtimeLines < 400, `runtime orchestrator stays under 400 lines (got ${runtimeLines})`)
})
