import assert from 'node:assert/strict'
import test from 'node:test'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { bundledCliPath, runCli, resolveDataDir, resolveCli } from '../lib/cli.js'

const pluginRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')

test('bundledCliPath points at this platform build', () => {
  const target = `${process.platform}-${process.arch}`
  const supported = ['win32-x64', 'win32-arm64', 'darwin-x64', 'darwin-arm64', 'linux-x64', 'linux-arm64']
  const bundled = bundledCliPath({ root: pluginRoot })
  if (!supported.includes(target)) return
  assert.ok(bundled, `expected a bundled CLI for ${target}`)
  assert.ok(bundled.includes(/^win32/.test(process.platform) ? '.exe' : 'aiprison'))
})

test('resolveCli prefers config, then AIPRISON_BIN, then the bundled build', () => {
  const bundled = bundledCliPath({ root: pluginRoot })
  if (bundled) {
    const resolved = resolveCli({ root: pluginRoot })
    assert.equal(resolved.source, 'bundled')
    assert.equal(resolved.path, bundled)
  }
  const viaEnv = resolveCli({ env: { AIPRISON_BIN: bundled || pluginRoot }, root: pluginRoot })
  assert.equal(viaEnv.source, 'AIPRISON_BIN')
  const viaConfig = resolveCli({ config: { cliPath: bundled || pluginRoot }, root: pluginRoot })
  assert.equal(viaConfig.source, 'config')
})

test('resolveCli reports a readable reason when nothing resolves', () => {
  const resolved = resolveCli({ env: {}, platform: 'aix', arch: 'ppc64', root: '/nonexistent-root' })
  assert.equal(resolved.path, undefined)
  assert.match(resolved.missing, /aiprison/)
})

test('resolveDataDir defaults to the working directory', () => {
  assert.equal(resolveDataDir({ cwd: path.join(pluginRoot, 'w') }), path.join(pluginRoot, 'w', '.ai-prison'))
  assert.equal(resolveDataDir({ config: { dataDir: '/tmp/ledger' }, cwd: '/tmp/w' }), '/tmp/ledger')
})

test('the bundled CLI answers version and levels', async () => {
  const bin = bundledCliPath({ root: pluginRoot })
  if (!bin) {
    test.skip('no bundled CLI for this platform')
    return
  }
  const version = await runCli(['version', '-v'], { bin })
  assert.equal(version.ok, true, version.stderr)
  assert.match(version.stdout, /aiprison\s+[0-9]/)
  const levels = await runCli(['levels'], { bin })
  assert.equal(levels.ok, true, levels.stderr)
  assert.match(levels.stdout, /L0 Warning/)
})

test('runCli never rejects on a bad binary', async () => {
  const result = await runCli(['version'], { bin: path.join(pluginRoot, 'no-such-binary') })
  assert.equal(result.ok, false)
  assert.ok(result.error)
})
