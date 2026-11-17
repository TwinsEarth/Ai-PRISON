import assert from 'node:assert/strict'
import fs from 'node:fs'
import path from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'
import { listSegments, resetLedger } from '../lib/ledger.js'

const pluginRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')

function makeLedger() {
  const dir = fs.mkdtempSync(path.join(pluginRoot, '.tmp-ledger-'))
  fs.writeFileSync(path.join(dir, 'seg-0000000000.log'), 'a'.repeat(64))
  fs.writeFileSync(path.join(dir, 'seg-0000000080.log'), 'b'.repeat(64))
  fs.writeFileSync(path.join(dir, 'notes.txt'), 'keep me')
  return dir
}

test('listSegments returns only the CLI segment files, in order', () => {
  const dir = makeLedger()
  try {
    const segments = listSegments(dir)
    assert.deepEqual(segments.map((s) => s.name), ['seg-0000000000.log', 'seg-0000000080.log'])
    assert.ok(segments.every((s) => s.bytes > 0))
    assert.deepEqual(listSegments(path.join(dir, 'missing')), [])
    assert.deepEqual(listSegments(undefined), [])
  } finally {
    fs.rmSync(dir, { recursive: true, force: true })
  }
})

test('resetLedger removes only seg-*.log and keeps unrelated files', () => {
  const dir = makeLedger()
  try {
    const result = resetLedger(dir)
    assert.equal(result.ok, true)
    assert.deepEqual(result.removed, ['seg-0000000000.log', 'seg-0000000080.log'])
    assert.deepEqual(fs.readdirSync(dir), ['notes.txt'])
    assert.deepEqual(listSegments(dir), [])
  } finally {
    fs.rmSync(dir, { recursive: true, force: true })
  }
})

test('resetLedger refuses a filesystem root and reports a missing directory', () => {
  const root = path.parse(process.cwd()).root
  assert.equal(resetLedger(root).ok, false)
  assert.match(resetLedger(root).error, /root/)
  assert.equal(resetLedger(undefined).ok, false)
  const dir = fs.mkdtempSync(path.join(pluginRoot, '.tmp-ledger-'))
  try {
    assert.equal(resetLedger(dir).ok, true)
    assert.deepEqual(resetLedger(dir).removed, [])
  } finally {
    fs.rmSync(dir, { recursive: true, force: true })
  }
})
