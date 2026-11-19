import assert from 'node:assert/strict'
import fs from 'node:fs'
import path from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const patch = fs.readFileSync(path.join(root, 'cordis.patch.yml'), 'utf8')
const loader = fs.readFileSync(path.join(root, 'watchdog.js'), 'utf8')
const readme = fs.readFileSync(path.join(root, 'README.md'), 'utf8')

test('the bundle patch no longer claims an observe-only, unimplemented enforcement', () => {
  for (const stale of [
    'P0 watchdog in OBSERVE mode',
    'enforcement arrives in later phases',
    'enforcement stays in later phases',
    'enforcement is not implemented',
    'P0 is observe-only; enforcement',
  ]) {
    assert.ok(!patch.includes(stale), `cordis.patch.yml still says: ${stale}`)
  }
  assert.match(patch, /Enforcement is ON by[\s#]+default/)
  assert.match(patch, /Set enforce: false/)
})

test('the patch documents every security-sensitive watchdog default', () => {
  for (const fragment of [
    'enforce: true',
    'rssScan: false',
    'probeAuto: false',
    'tamperPolicy: observe',
    'crashFallback: enforce',
    'notify: false',
  ]) {
    assert.ok(patch.includes(fragment), `cordis.patch.yml documents ${fragment}`)
  }
})

test('the patch still inserts exactly the two expected rows', () => {
  assert.match(patch, /- id: ai-prison\n\s+name: '@twinsearth\/ai-prison-dsh-plugin'\n\s+config: \{\}/)
  assert.match(patch, /- id: ai-prison-watchdog\n\s+name: '@twinsearth\/ai-prison-dsh-plugin\/watchdog'\n\s+config: \{\}/)
  assert.doesNotMatch(patch, /disabled:\s*true/)
})

test('the loader comment and README describe the enforce-first behavior', () => {
  assert.ok(!loader.includes('P0: observe only'), 'watchdog.js loader comment is current')
  assert.match(loader, /Enforcement is on by default/)
  assert.match(readme, /Enforcement is on by default/)
  assert.match(readme, /config\.enforce: false/)
})

test('README, package metadata, locale and market agree on the eight-tool surface', () => {
  const toolsSource = fs.readFileSync(path.join(root, 'lib', 'tools.js'), 'utf8')
  const registered = [...new Set([...toolsSource.matchAll(/name: '(ai_prison_[a-z_]+)'/g)].map((match) => match[1]))].sort()
  assert.equal(registered.length, 8, `tools.js registers ${registered.length} tools`)

  const documented = [...new Set([...readme.matchAll(/\| `(ai_prison_[a-z_]+)` \|/g)].map((match) => match[1]))].sort()
  assert.deepEqual(documented, registered, 'the README tool table lists every registered tool')
  assert.match(readme, /eight `ai_prison_\*` tools/)
  assert.match(readme, /the eight tools become visible/)
  assert.doesNotMatch(readme, /six `ai_prison_\*` tools|the six tools become visible/)

  const pkg = JSON.parse(fs.readFileSync(path.join(root, 'package.json'), 'utf8'))
  assert.match(pkg.description, /eight governance tools/)

  const en = JSON.parse(fs.readFileSync(path.join(root, 'locale', 'en.json'), 'utf8'))
  const zh = JSON.parse(fs.readFileSync(path.join(root, 'locale', 'zh.json'), 'utf8'))
  for (const description of [en.meta.description, zh.meta.description]) {
    assert.match(description, /watchdog status/)
  }
  assert.match(en.meta.description, /eight DSH tools/)
  assert.match(zh.meta.description, /八个 DSH 工具/)

  const market = JSON.parse(fs.readFileSync(path.join(root, 'market', 'index.json'), 'utf8'))
  const entry = market.plugins.find((plugin) => plugin.name === pkg.name)
  assert.equal(entry.description.en, en.meta.description)
  assert.equal(entry.description.zh, zh.meta.description)
})
