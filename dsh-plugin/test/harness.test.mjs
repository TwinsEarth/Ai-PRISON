import assert from 'node:assert/strict'
import test from 'node:test'
import { installationAnchors } from '../lib/harness.js'

test('installation anchors include the extracted runtime from DSH_INSTALL_DIR', () => {
  const anchors = installationAnchors({
    execPath: 'E:/DSH/DS/resources/runtime/primary-runtime/dependencies/node/bin/node.exe',
    env: { DSH_INSTALL_DIR: 'E:/DSH/DS/resources/dsh-runtime/dsh' },
    argv: ['node', 'entry.js', 'E:/DSH/DS/resources/dsh-runtime/dsh', 'profile'],
  })
  assert.ok(anchors.some((anchor) => anchor.endsWith('dsh-runtime\\dsh\\package.json')))
  assert.equal(anchors[0], 'E:\\DSH\\DS\\resources\\dsh-runtime\\dsh\\package.json')
})

test('installation anchors fall back to the host runtime argument', () => {
  const anchors = installationAnchors({
    execPath: 'E:/DSH/DS/resources/runtime/primary-runtime/dependencies/node/bin/node.exe',
    env: {},
    argv: ['node', 'entry.js', 'E:/DSH/DS/resources/dsh-runtime/dsh', 'profile'],
  })
  assert.ok(anchors.some((anchor) => anchor.endsWith('dsh-runtime\\dsh\\package.json')))
})

test('electron-as-node anchors still point at the desktop asar', () => {
  const anchors = installationAnchors({
    execPath: 'E:/DSH/DS/DeepSeek Harness.exe',
    env: {},
    argv: ['Electron', 'entry.js'],
  })
  assert.ok(anchors.some((anchor) => anchor.endsWith('resources\\app.asar\\dsh\\package.json')))
})
