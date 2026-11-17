import assert from 'node:assert/strict'
import crypto from 'node:crypto'
import fs from 'node:fs'
import path from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const pluginRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const pkg = JSON.parse(fs.readFileSync(path.join(pluginRoot, 'package.json'), 'utf8'))
const market = JSON.parse(fs.readFileSync(path.join(pluginRoot, 'market/index.json'), 'utf8'))

test('manifest declares the bundle, the client half and the version gates', () => {
  assert.equal(pkg.name, '@twinsearth/ai-prison-dsh-plugin')
  assert.equal(pkg.version, '1.1.0')
  assert.equal(pkg.type, 'module')
  assert.deepEqual(pkg.dsh.bundle, { patch: './cordis.patch.yml' })
  assert.equal(pkg.dsh.client.platform, 'web')
  assert.equal(pkg.dsh.client.immediately, true)
  assert.ok(pkg.dsh.client.inject.includes('@deepseek-ai/dsh-client-locale'))
  assert.ok(pkg.engines.dsh.includes('0.2.2-rc.3'), 'engines.dsh names the current runtime line')
  assert.ok(pkg.engines.node.includes('20'))
  assert.equal(pkg.publishConfig.access, 'public')
})

test('exports resolve to real files', () => {
  for (const [name, target] of Object.entries(pkg.exports)) {
    const relative = String(target).replace(/^\.\//, '')
    if (relative.includes('*')) {
      const prefix = path.join(pluginRoot, relative.split('*')[0])
      const dir = path.dirname(prefix)
      assert.ok(fs.existsSync(dir), `${name} points at a missing directory`)
      continue
    }
    assert.ok(fs.existsSync(path.join(pluginRoot, relative)), `${name} -> ${target} missing`)
  }
})

test('every file the runtime needs is listed in files', () => {
  const required = ['index.js', 'client.js', 'cordis.patch.yml', 'icon.svg', 'docs', 'bin', 'locale']
  const declared = pkg.files.join('|')
  for (const entry of required) assert.ok(declared.includes(entry), `${entry} is not packaged`)
})

test('the patch inserts one enabled row for this package', () => {
  const patch = fs.readFileSync(path.join(pluginRoot, 'cordis.patch.yml'), 'utf8')
  assert.match(patch, /id:\s*ai-prison/) 
  assert.match(patch, /name:\s*'@twinsearth\/ai-prison-dsh-plugin'/)
  assert.doesNotMatch(patch, /disabled:\s*true/)
})

test('locale metadata matches the package name and ships detail documents', () => {
  const en = JSON.parse(fs.readFileSync(path.join(pluginRoot, 'locale/en.json'), 'utf8'))
  const zh = JSON.parse(fs.readFileSync(path.join(pluginRoot, 'locale/zh.json'), 'utf8'))
  assert.ok(en.meta.title.length > 0)
  assert.ok(en.meta.description.length > 40, 'description explains capability, usage and limits')
  assert.equal(en.meta.detail, 'docs/DETAIL.md')
  assert.equal(zh.meta.detail, 'docs/DETAIL.zh.md')
  assert.ok(zh.meta.title.includes('AI'))
  for (const [locale, meta] of [['en', en], ['zh', zh]]) {
    const doc = path.join(pluginRoot, meta.meta.detail)
    assert.ok(fs.existsSync(doc), `${locale} detail document missing`)
    const body = fs.readFileSync(doc, 'utf8').trim().split(/\r?\n/)
    assert.equal(body.length, 1, `${locale} detail document holds exactly one image line`)
    assert.match(body[0], /^!\[[^\]]+\]\([^)]+\)$/)
    const image = body[0].match(/\(([^)]+)\)/)[1]
    assert.ok(fs.existsSync(path.join(pluginRoot, path.dirname(meta.meta.detail), image)), `${locale} preview image missing`)
  }
})

test('icons and previews stay inside the documented size limits', () => {
  const icon = fs.readFileSync(path.join(pluginRoot, 'icon.svg'))
  assert.ok(icon.length <= 256 * 1024, `icon is ${icon.length} bytes`)
  assert.match(icon.toString('utf8'), /^<svg[\s>]/)
  assert.doesNotMatch(icon.toString('utf8'), /<script|<text|<image|<foreignObject/)
  const preview = fs.readFileSync(path.join(pluginRoot, 'docs/preview.svg'))
  assert.ok(preview.length <= 1024 * 1024)
  assert.match(preview.toString('utf8'), /^<svg[\s>]/)
})

test('the market index advertises the same name, version and engine range', () => {
  assert.equal(market.format, 1)
  const entry = market.plugins.find((plugin) => plugin.name === pkg.name)
  assert.ok(entry, 'market index lists the plugin')
  assert.equal(entry.versions.length, 1)
  assert.equal(entry.versions[0].version, pkg.version)
  assert.equal(entry.versions[0].engines.dsh, pkg.engines.dsh)
  assert.deepEqual(entry.versions[0].source, { type: 'npm', registry: 'https://registry.npmjs.org' })
  assert.ok(entry.versions[0].publishTimestamp > 1_700_000_000)
  assert.match(entry.icon, /^data:image\/svg\+xml;base64,/)
  const decoded = Buffer.from(entry.icon.split(',')[1], 'base64').toString('utf8')
  assert.equal(decoded.trim(), fs.readFileSync(path.join(pluginRoot, 'icon.svg'), 'utf8').trim())
})

test('bundled binaries for all six targets exist and are distinct builds', () => {
  const targets = pkg.files.includes('bin') ? ['windows-amd64', 'windows-arm64', 'macos-amd64', 'macos-arm64', 'linux-amd64', 'linux-arm64'] : []
  const hashes = new Map()
  for (const target of targets) {
    const exe = target.startsWith('windows') ? 'aiprison.exe' : 'aiprison'
    const file = path.join(pluginRoot, 'bin', target, exe)
    assert.ok(fs.existsSync(file), `missing bundled binary ${target}`)
    const bytes = fs.readFileSync(file)
    assert.ok(bytes.length > 1024 * 1024, `${target} binary looks too small`)
    const digest = crypto.createHash('sha256').update(bytes).digest('hex')
    assert.ok(!hashes.has(digest), `${target} duplicates ${hashes.get(digest)}`)
    hashes.set(digest, target)
  }
  assert.equal(hashes.size, targets.length)
})
