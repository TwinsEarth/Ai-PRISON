import assert from 'node:assert/strict'
import path from 'node:path'
import test from 'node:test'
import { installationAnchors } from '../lib/harness.js'

const RUNTIME_ROOT = path.join('E:', 'DSH', 'DS', 'resources', 'dsh-runtime', 'dsh')
const NODE_EXE = path.join('E:', 'DSH', 'DS', 'resources', 'runtime', 'primary-runtime', 'dependencies', 'node', 'bin', 'node.exe')
const DESKTOP_EXE = path.join('E:', 'DSH', 'DS', 'DeepSeek Harness.exe')

test('installation anchors include the extracted runtime from DSH_INSTALL_DIR', () => {
  const anchors = installationAnchors({
    execPath: NODE_EXE,
    env: { DSH_INSTALL_DIR: RUNTIME_ROOT },
    argv: ['node', 'entry.js', RUNTIME_ROOT, 'profile'],
  })
  assert.equal(anchors[0], path.join(RUNTIME_ROOT, 'package.json'))
})

test('installation anchors fall back to the host runtime argument', () => {
  const anchors = installationAnchors({
    execPath: NODE_EXE,
    env: {},
    argv: ['node', 'entry.js', RUNTIME_ROOT, 'profile'],
  })
  assert.ok(anchors.some((anchor) => anchor === path.join(RUNTIME_ROOT, 'package.json')))
})

test('electron-as-node anchors still point at the desktop asar', () => {
  const anchors = installationAnchors({
    execPath: DESKTOP_EXE,
    env: {},
    argv: ['Electron', 'entry.js'],
  })
  assert.ok(anchors.some((anchor) => anchor === path.join(path.dirname(DESKTOP_EXE), 'resources', 'app.asar', 'dsh', 'package.json')))
})

test('installation anchors follow the entry module in the runtime root', () => {
  const entry = path.join(RUNTIME_ROOT, 'index.js')
  const anchors = installationAnchors({ execPath: NODE_EXE, env: {}, argv: ['node', entry] })
  assert.ok(anchors.includes(path.join(RUNTIME_ROOT, 'package.json')), 'entry dir anchor present')
})

test('installation anchors follow a nested entry module back to the package root', () => {
  const entry = path.join(RUNTIME_ROOT, 'dist', 'index.js')
  const anchors = installationAnchors({ execPath: NODE_EXE, env: {}, argv: ['node', entry] })
  assert.ok(anchors.includes(path.join(RUNTIME_ROOT, 'package.json')), 'entry parent anchor present')
})

test('a non-DSH entry path does not add a bogus anchor', () => {
  const anchors = installationAnchors({ execPath: NODE_EXE, env: {}, argv: ['node', path.join('C:', 'tools', 'other', 'index.js')] })
  assert.ok(!anchors.some((anchor) => anchor === path.join('C:', 'tools', 'other', 'package.json')))
})
