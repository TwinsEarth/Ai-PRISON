import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'
import { bundledCliPath, runCli } from '../lib/cli.js'
import { buildStatus, inspectLedger, statusSummary } from '../lib/status.js'

const pluginRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const bin = bundledCliPath({ root: pluginRoot })

function tempWorkspace(prefix) {
  const dir = fs.mkdtempSync(path.join(pluginRoot, `.tmp-${prefix}-`))
  return dir
}

function cleanup(dir) {
  try {
    fs.rmSync(dir, { recursive: true, force: true })
  } catch {
    // best effort
  }
}

test('a missing ledger reads as not initialized', async () => {
  const cwd = tempWorkspace('status-none')
  try {
    const ledger = await inspectLedger({ bin, dataDir: path.join(cwd, '.ai-prison') })
    assert.equal(ledger.exists, false)
    assert.equal(ledger.integrity, 'none')
    assert.equal(ledger.records, null)
  } finally {
    cleanup(cwd)
  }
})

test('demo writes a ledger that verify reports as intact', async (t) => {
  if (!bin) {
    t.skip('no bundled CLI for this platform')
    return
  }
  const cwd = tempWorkspace('status-demo')
  try {
    const dataDir = path.join(cwd, '.ai-prison')
    const demo = await runCli(['--data-dir', dataDir, 'demo'], { bin, cwd })
    assert.equal(demo.ok, true, `${demo.stdout}\n${demo.stderr}`)
    const ledger = await inspectLedger({ bin, dataDir })
    assert.equal(ledger.exists, true)
    assert.equal(ledger.integrity, 'ok', ledger.detail)
    assert.ok(ledger.records > 0, `expected records, got ${ledger.records}`)
    assert.ok(ledger.files > 0)
  } finally {
    cleanup(cwd)
  }
})

test('buildStatus reports CLI, matrix and ledger together', async (t) => {
  if (!bin) {
    t.skip('no bundled CLI for this platform')
    return
  }
  const cwd = tempWorkspace('status-build')
  try {
    const dataDir = path.join(cwd, '.ai-prison')
    const demo = await runCli(['--data-dir', dataDir, 'demo'], { bin, cwd })
    assert.equal(demo.ok, true, demo.stderr)
    const status = await buildStatus({ cwd, config: { dataDir } })
    assert.equal(status.ok, true)
    assert.equal(status.cli.found, true)
    assert.match(status.cli.version, /[0-9]/)
    assert.equal(status.levels.length, 6, 'the matrix has six levels')
    assert.equal(status.levels[0].code, 'L0')
    assert.equal(status.levels[5].code, 'L5')
    assert.equal(status.ledger.integrity, 'ok')
    assert.ok(status.ledger.records > 0)
    const summary = statusSummary(status)
    assert.match(summary, /INTEGRITY OK/)
    assert.match(summary, /6 级/)
  } finally {
    cleanup(cwd)
  }
})

test('a corrupt ledger is reported as a failed check, not an exception', async (t) => {
  if (!bin) {
    t.skip('no bundled CLI for this platform')
    return
  }
  const cwd = tempWorkspace('status-corrupt')
  try {
    const dataDir = path.join(cwd, '.ai-prison')
    const demo = await runCli(['--data-dir', dataDir, 'demo'], { bin, cwd })
    assert.equal(demo.ok, true, demo.stderr)
    const files = fs.readdirSync(dataDir)
    const segment = files.find((name) => name.endsWith('.jsonl')) || files[0]
    const target = path.join(dataDir, segment)
    const original = fs.readFileSync(target, 'utf8')
    const lines = original.split('\n')
    if (lines.length > 1) {
      const index = Math.floor(lines.length / 2)
      lines[index] = lines[index].replace(/"seq":(\d+)/, '"seq":not-a-number')
      fs.writeFileSync(target, lines.join('\n'))
      const ledger = await inspectLedger({ bin, dataDir })
      assert.equal(ledger.integrity, 'failed')
      assert.ok(ledger.detail.length > 0)
    }
  } finally {
    cleanup(cwd)
  }
})

test('os.tmpdir stays out of the bundled-CLI path', () => {
  // The CLI is resolved from the package, never from the ambient PATH in tests.
  const resolved = bin || ''
  assert.ok(!resolved.includes(os.tmpdir()) || resolved.startsWith(pluginRoot))
})
