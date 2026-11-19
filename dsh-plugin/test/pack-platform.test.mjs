import assert from 'node:assert/strict'
import path from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'
import { parseArgs } from '../scripts/pack-platform.mjs'

const pluginRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')

test('pack-platform defaults to the windows-amd64 target in dist/platform-packs', () => {
  const args = parseArgs([], { root: pluginRoot })
  assert.deepEqual(args.targets, ['windows-amd64'])
  assert.equal(args.dryRun, false)
  assert.ok(args.out.endsWith(path.join('dist', 'platform-packs')))
})

test('pack-platform --all expands to the six bundled targets', () => {
  const args = parseArgs(['--all', '--dry-run'], { root: pluginRoot })
  assert.equal(args.targets.length, 6)
  assert.ok(args.targets.includes('windows-amd64'))
  assert.ok(args.targets.includes('macos-arm64'))
  assert.ok(args.targets.includes('linux-amd64'))
  assert.equal(args.dryRun, true)
})

test('pack-platform accepts an explicit platform/arch pair and rejects unknown targets', () => {
  const args = parseArgs(['--platform', 'macos', '--arch', 'arm64'], { root: pluginRoot })
  assert.deepEqual(args.targets, ['macos-arm64'])
  assert.throws(() => parseArgs(['--platform', 'solaris', '--arch', 'sparc'], { root: pluginRoot }), /unknown target/)
  assert.throws(() => parseArgs(['--bogus'], { root: pluginRoot }), /unknown argument/)
})
