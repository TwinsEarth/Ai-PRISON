import assert from 'node:assert/strict'
import fs from 'node:fs'
import path from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const source = fs.readFileSync(path.join(root, 'lib', 'watchdog-runtime.js'), 'utf8')

test('watchdog-runtime imports every helper module it uses', () => {
  for (const [name, specifier] of [
    ['createBootWatch', './boot-watch.js'],
    ['startRssScannerV2', './rss-scan.js'],
    ['createTamperMonitor', './tamper-monitor.js'],
    ['remediationFor', './remediation.js'],
    ['notify', './notifier.js'],
    // parseProcessList lives in rss-scan.js; only require that file to import it
  ]) {
    assert.ok(source.includes(`from '${specifier}'`), `${name} must import ${specifier}`)
    assert.ok(source.includes(`${name}(`) || source.includes(`${name}:`), `${name} is used`)
  }
})
