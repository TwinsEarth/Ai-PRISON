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
