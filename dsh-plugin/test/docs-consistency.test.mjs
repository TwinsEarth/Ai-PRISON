import assert from 'node:assert/strict'
import fs from 'node:fs'
import path from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'
import { thresholdsFor, GB } from '../lib/watchdog-policy.js'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const read = (name) => fs.readFileSync(path.join(root, name), 'utf8')

test('README numbers match thresholdsFor', () => {
  const readme = read('README.md')
  const six = thresholdsFor(6 * GB)
  const four = thresholdsFor(4 * GB)
  assert.ok(readme.includes('nominal 3.0/3.9/5.0'))
  assert.ok(readme.includes('effective 2.7/3.6/4.7'))
  assert.ok(readme.includes('nominal 2.92/3.52/3.72'))
  assert.ok(readme.includes('effective 2.62/3.22/3.42'))
  assert.ok(readme.includes('6.8 MB packed, 16.1 MB unpacked'))
  assert.equal((six.totalWarn / GB).toFixed(1), '3.0')
  assert.equal((six.totalPause / GB).toFixed(1), '3.9')
  assert.equal((six.totalBlock / GB).toFixed(1), '5.0')
  assert.equal((four.totalWarn / GB).toFixed(2), '2.92')
  assert.equal((four.totalPause / GB).toFixed(2), '3.52')
  assert.equal((four.totalBlock / GB).toFixed(2), '3.72')
})

test('SUPPLY-CHAIN sizes match the measured npm tarball', () => {
  const supply = read('docs/SUPPLY-CHAIN.md')
  assert.ok(supply.includes('6.8 MB tgz (16.1 MB unpacked'))
  assert.ok(supply.includes('2.6–2.8 MB'))
  assert.ok(supply.includes('not the same bytes as one all-platform tarball'))
})
