import assert from 'node:assert/strict'
import { EventEmitter } from 'node:events'
import test from 'node:test'
import { parseProcessList } from '../lib/rss.js'
import { scanViaService, startRssScannerV2 } from '../lib/rss-scan.js'

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

function makeScanner({ spawn, config = { rssScan: true }, ctx } = {}) {
  const incidents = []
  const watchdogState = { config, rss: {} }
  const refs = { manager: { listBundles: async () => [{ name: '@a/b', enabled: true }, { name: 'base', enabled: true }] } }
  const scanner = startRssScannerV2({
    ctx: ctx || { get: () => null },
    refs,
    profileDir: 'C:/prof/desktop',
    watchdogState,
    appendIncident: (incident) => incidents.push(incident),
    exemptTargets: new Set(['base']),
    spawn,
  })
  return { scanner, watchdogState, incidents, refs }
}

test('the child RSS scan is opt-in: default and rssScan:false never start it', () => {
  for (const config of [{}, { rssScan: false }, { rssScan: 'yes' }, undefined]) {
    const watchdogState = { config, rss: {} }
    const refs = {}
    const scanner = startRssScannerV2({
      ctx: { get: () => null },
      refs,
      profileDir: 'C:/prof/desktop',
      watchdogState,
      appendIncident: () => {},
      exemptTargets: new Set(),
      spawn: () => {
        throw new Error('direct spawn must never be reached for a disabled scanner')
      },
    })
    assert.equal(scanner, undefined, `config ${JSON.stringify(config)} keeps the scanner off`)
    assert.equal(refs.rssStarted, undefined)
  }
})

test('the scanner starts only when rssScan is exactly true', { skip: process.platform !== 'win32' }, () => {
  const { scanner, refs } = makeScanner({ spawn: () => fakeChild(() => {}) })
  try {
    assert.ok(scanner && typeof scanner.scan === 'function')
    assert.equal(scanner.timer !== null && scanner.timer !== undefined, true)
    assert.equal(refs.rssStarted, true)
  } finally {
    clearTimeout(scanner ? scanner.timer : undefined)
    clearInterval(scanner ? scanner.timer : undefined)
  }
})

function fakeService({ stdout = '', stderr = '', fail = false } = {}) {
  const calls = []
  return {
    calls,
    spawn(spec) {
      calls.push(spec)
      return {
        done: fail ? Promise.reject(new Error('service rejected the spawn')) : Promise.resolve({ exitCode: 0 }),
        collected: {
          stdout: { readFrom: (offset) => ({ text: stdout, offset }) },
          stderr: { readFrom: (offset) => ({ text: stderr, offset }) },
        },
      }
    },
  }
}

test('the subprocess-service path supplies the collected payload and the documented spec', async () => {
  const service = fakeService({ stdout: '111|1048576|node.exe C:\\prof\\desktop\\node_modules\\@a\\b\\worker.exe' })
  const result = await scanViaService(service, { profileDir: 'C:/prof/desktop' })
  assert.equal(result.stdout, '111|1048576|node.exe C:\\prof\\desktop\\node_modules\\@a\\b\\worker.exe')
  const spec = service.calls[0]
  assert.ok(Array.isArray(spec.argv) && spec.argv[0] === 'powershell.exe')
  assert.equal(spec.cwd, 'C:/prof/desktop')
  assert.equal(spec.stdio.stdin, 'ignore')
  assert.equal(spec.stdio.stdout.maxBytes, 4 * 1024 * 1024)
  assert.equal(spec.graceMs, 5000)
  assert.ok(spec.signal instanceof AbortSignal)
})

test('the subprocess-service path fails loudly on empty output or provider failure', async () => {
  const empty = fakeService({ stdout: '', stderr: 'no access' })
  await assert.rejects(() => scanViaService(empty), /returned no stdout: no access/)
  const broken = fakeService({ fail: true })
  await assert.rejects(() => scanViaService(broken), /service rejected the spawn/)
})

test('a scan prefers the subprocess service and falls back to a direct spawn', { skip: process.platform !== 'win32' }, async () => {
  let directCalls = 0
  const direct = () => {
    directCalls += 1
    return fakeChild((child) => {
      child.stdout.emit('data', '222|2097152|node.exe C:\\prof\\desktop\\node_modules\\@a\\b\\worker.exe')
      child.emit('exit', 0)
    })
  }
  const service = fakeService({ stdout: '111|1048576|node.exe C:\\prof\\desktop\\node_modules\\@a\\b\\worker.exe' })
  const viaService = makeScanner({ spawn: direct, ctx: { get: () => service } })
  try {
    await viaService.scanner.scan()
    assert.equal(directCalls, 0, 'the service payload is used first')
    assert.equal(viaService.watchdogState.rss['@a/b'], 1048576)
    assert.equal(viaService.watchdogState.rssServiceError, undefined)
  } finally {
    clearInterval(viaService.scanner.timer)
  }

  const broken = fakeService({ fail: true })
  const fallback = makeScanner({ spawn: direct, ctx: { get: () => broken } })
  try {
    await fallback.scanner.scan()
    assert.equal(directCalls, 1, 'a failing service falls back to the direct spawn')
    assert.equal(fallback.watchdogState.rss['@a/b'], 2097152)
    assert.match(fallback.watchdogState.rssServiceError, /service rejected the spawn/)
  } finally {
    clearInterval(fallback.scanner.timer)
  }
})

test('successful direct scan records matched RSS and stats', { skip: process.platform !== 'win32' }, async () => {
  const spawn = () =>
    fakeChild((child) => {
      child.stdout.emit('data', '111|1048576|node.exe C:\\prof\\desktop\\node_modules\\@a\\b\\worker.exe')
      child.emit('exit', 0)
    })
  const { scanner, watchdogState } = makeScanner({ spawn })
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

test('a failed scan is recorded and reported once', { skip: process.platform !== 'win32' }, async () => {
  const spawn = () =>
    fakeChild((child) => {
      child.stderr.emit('data', 'Access denied')
      child.emit('exit', 1)
    })
  const { scanner, watchdogState, incidents } = makeScanner({ spawn })
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
