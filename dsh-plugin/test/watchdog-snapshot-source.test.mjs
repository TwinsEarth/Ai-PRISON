import assert from 'node:assert/strict'
import fs from 'node:fs'
import path from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')

test('the snapshot computes Top10 once and slices Top5 from it', () => {
  const source = fs.readFileSync(path.join(root, 'lib', 'watchdog-snapshot.js'), 'utf8')
  assert.match(source, /const ranked = mergedTop\(s, 10\)/)
  assert.match(source, /ranked\.slice\(0, 5\)/)
  assert.doesNotMatch(source, /mergedTop\(s, 5\)/)
})
