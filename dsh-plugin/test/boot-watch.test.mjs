import assert from 'node:assert/strict'
import fs from 'node:fs'
import path from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'
import { createBootWatch, trimRunFiles, bootThresholdsFor } from '../lib/boot-watch.js'

const pluginRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')

function fakeProcess(heapMB) {
  return { pid: 4242, memoryUsage: () => ({ heapUsed: heapMB * 1024 * 1024, heapTotal: heapMB * 1024 * 1024, rss: heapMB * 1024 * 1024 }) }
}

test('startup thresholds depend on the heap cap', () => {
  assert.equal(bootThresholdsFor(4 * 1024 * 1024 * 1024).totalMB, 2000)
  assert.equal(bootThresholdsFor(6 * 1024 * 1024 * 1024).totalMB, 3000)
})

test('trimRunFiles keeps only the newest run files', () => {
  const dir = fs.mkdtempSync(path.join(pluginRoot, '.tmp-boot-'))
  try {
    for (let index = 0; index < 8; index += 1) fs.writeFileSync(path.join(dir, `run-${1000 + index}-1.jsonl`), '{}\n')
    trimRunFiles(dir, 3)
    const left = fs.readdirSync(dir).filter((name) => name.startsWith('run-')).sort()
    assert.equal(left.length, 3)
    assert.ok(left.every((name) => !name.startsWith('run-1000-') && !name.startsWith('run-1001-')))
  } finally {
    fs.rmSync(dir, { recursive: true, force: true })
  }
})

test('a startup heap spike triggers one safe-boot plan and records the curve', () => {
  const dir = fs.mkdtempSync(path.join(pluginRoot, '.tmp-boot-'))
  const watchdogState = { baseline: null, thresholds: { heapLimit: 4 * 1024 * 1024 * 1024 } }
  const triggers = []
  const fake = fakeProcess(10)
  const watch = createBootWatch({
    dataDir: dir,
    processLike: fake,
    watchdogState,
    sampleMs: 999_999,
    onTrigger: (trigger) => triggers.push(trigger),
  })
  try {
    fake.memoryUsage = () => ({ heapUsed: 2300 * 1024 * 1024, heapTotal: 2400 * 1024 * 1024, rss: 2500 * 1024 * 1024 })
    watch.tick()
    const summary = watch.stop('test')
    assert.equal(triggers.length, 1, 'exactly one trigger')
    assert.match(triggers[0].reason, /2300 MB/)
    const lines = fs.readFileSync(path.join(dir, watch.runFile.replace(/^.*[\\/]/, '')), 'utf8').trim().split('\n').map((line) => JSON.parse(line))
    assert.ok(lines.length >= 2)
    assert.equal(summary.samples >= 2, true)
    assert.equal(summary.triggered.totalMB, 2300)
  } finally {
    fs.rmSync(dir, { recursive: true, force: true })
  }
})

test('a clean baseline finishes the watch without triggering', () => {
  const dir = fs.mkdtempSync(path.join(pluginRoot, '.tmp-boot-'))
  const watchdogState = { baseline: null, thresholds: { heapLimit: 4 * 1024 * 1024 * 1024 } }
  const triggers = []
  const watch = createBootWatch({ dataDir: dir, processLike: fakeProcess(10), watchdogState, sampleMs: 999_999, onTrigger: (trigger) => triggers.push(trigger) })
  try {
    watchdogState.baseline = { at: new Date().toISOString(), bundles: [] }
    watch.tick()
    assert.equal(triggers.length, 0)
  } finally {
    fs.rmSync(dir, { recursive: true, force: true })
  }
})
