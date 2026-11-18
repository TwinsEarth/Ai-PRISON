import assert from 'node:assert/strict'
import { EventEmitter } from 'node:events'
import test from 'node:test'
import { parseProcessList } from '../lib/rss.js'
import { startRssScannerV2 } from '../lib/rss-scan.js'

const names = ['@a/b', '@c/d']

test('parser attributes windows and posix package paths and skips host/self', () => {
  const output = [
    '100|1048576|node.exe C:\\prof\\desktop\\node_modules\\@a\\b\\bin\\worker.exe --run',
    '101|2097152|node.exe C:/prof/desktop/node_modules/@c/d/lib/index.js',
    '4242|999|node.exe E:\\DSH\\dsh-desktop-host\\index.js',
    `${process.pid}|888|node.exe C:\\prof\\desktop\\node_modules\\@a\\b\\self`,
    'not-a-line',
  ].join('\n')
  const parsed = parseProcessList(output, names)
  assert.equal(parsed.rss['@a/b'], 1048576)
  assert.equal(parsed.rss['@c/d'], 2097152)
  assert.equal(parsed.matched, 2)
  assert.equal(parsed.lines, 4)
})

function fakeChild(run) {
  const child = new EventEmitter()
  child.stdout = new EventEmitter()
  child.stderr = new EventEmitter()
  child.kill = () => {}
  setImmediate(() => run(child))
  return child
}

function makeScanner(spawn) {
  const incidents = []
  const watchdogState = { config: {}, rss: {} }
  const refs = { manager: { listBundles: async () => [{ name: '@a/b', enabled: true }, { name: 'base', enabled: true }] } }
  const ctx = { get: () => null }
  const scanner = startRssScannerV2({
    ctx,
    refs,
    profileDir: 'C:/prof/desktop',
    watchdogState,
    appendIncident: (incident) => incidents.push(incident),
    exemptTargets: new Set(['base']),
    spawn,
  })
  return { scanner, watchdogState, incidents, refs }
}

test('successful scan records matched RSS and stats', async () => {
  const spawn = () =>
    fakeChild((child) => {
      child.stdout.emit('data', '111|1048576|node.exe C:\\prof\\desktop\\node_modules\\@a\\b\\worker.exe')
      child.emit('exit', 0)
    })
  const { scanner, watchdogState } = makeScanner(spawn)
  try {
    await scanner.scan()
    assert.equal(watchdogState.rss['@a/b'], 1048576)
    assert.equal(watchdogState.rssStats.matched, 1)
    assert.equal(watchdogState.rssError, null)
    assert.ok(watchdogState.rssAt)
  } finally {
    clearInterval(scanner.timer)
  }
})

test('a failed scan is recorded and reported once', async () => {
  const spawn = () =>
    fakeChild((child) => {
      child.stderr.emit('data', 'Access denied')
      child.emit('exit', 1)
    })
  const { scanner, watchdogState, incidents } = makeScanner(spawn)
  try {
    await scanner.scan()
    await scanner.scan()
    assert.match(watchdogState.rssError, /powershell exit 1/)
    assert.match(watchdogState.degraded, /rss scan/)
    assert.equal(incidents.filter((incident) => incident.kind === 'rss-scan-failed').length, 1)
  } finally {
    clearInterval(scanner.timer)
  }
})
