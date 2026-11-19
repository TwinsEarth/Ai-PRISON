import assert from 'node:assert/strict'
import fs from 'node:fs'
import path from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'
import { PLUGIN_VERSION } from '../lib/version.js'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')

test('the panel version follows package.json, not the bundled CLI version', () => {
  const pkg = JSON.parse(fs.readFileSync(path.join(root, 'package.json'), 'utf8'))
  assert.equal(PLUGIN_VERSION, pkg.version)
  assert.match(PLUGIN_VERSION, /^\d+\.\d+\.\d+$/)
})
