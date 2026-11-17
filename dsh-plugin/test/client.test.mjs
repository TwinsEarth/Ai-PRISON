import assert from 'node:assert/strict'
import path from 'node:path'
import test from 'node:test'
import { fileURLToPath, pathToFileURL } from 'node:url'

const pluginRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const clientFile = path.join(pluginRoot, 'client.js')

test('the client bundle registers the sidebar entry and the main panel', async () => {
  const loaded = []
  globalThis.window = { __ModuleLoader__: { load: (spec) => loaded.push(spec) } }
  if (!globalThis.navigator) globalThis.navigator = { language: 'zh-CN' }
  try {
    await import(`${pathToFileURL(clientFile).href}?test=${Date.now()}`)
  } finally {
    delete globalThis.window
  }
  assert.equal(loaded.length, 1, 'client.js loads exactly one module')
  const spec = loaded[0]
  assert.equal(spec.id, '@twinsearth/ai-prison-dsh-plugin')
  assert.equal(typeof spec.factory, 'function')

  const React = { createElement: () => null }
  const require_ = (name) => {
    if (name === 'react') return React
    throw new Error(`unexpected require(${JSON.stringify(name)})`)
  }
  const plugin = spec.factory(require_)
  assert.deepEqual(plugin.inject, ['slots'])
  assert.equal(plugin.PANEL_ID, 'ai-prison')
  assert.equal(plugin.NS, 'ai-prison')

  const slots = []
  const locales = []
  const effects = []
  const ctx = {
    effect: (fn) => {
      effects.push(fn)
      fn()
      return () => {}
    },
    slots: {
      inject: (name, register) => {
        slots.push({ inject: name })
        register()
        return () => {}
      },
      register: (options, component) => {
        slots.push({ register: options, component })
        return () => {}
      },
    },
    locale: {
      register: (ns, locale, dict) => {
        locales.push({ ns, locale, keys: Object.keys(dict).length })
        return () => {}
      },
      bind: () => (key) => key,
    },
  }
  plugin.apply(ctx)

  const sidebar = slots.find((entry) => entry.register?.name === 'sidebar.panellist')
  assert.ok(sidebar, 'sidebar entry registered')
  assert.equal(sidebar.register.id, 'ai-prison')
  assert.equal(typeof sidebar.register.label, 'function')

  const panel = slots.find((entry) => entry.register?.name === 'main')
  assert.ok(panel, 'main panel registered')
  assert.equal(panel.register.key, 'ai-prison')
  assert.equal(typeof panel.component, 'function', 'panel component is a React component')

  assert.equal(locales.length, 2, 'both locale dictionaries are registered')
  for (const entry of locales) assert.ok(entry.keys > 20, `${entry.locale} dictionary looks complete`)
  assert.ok(effects.length >= 2, 'slot registrations are effect-owned for disposal')
})

test('the client bundle reads the status API with same-origin credentials', async () => {
  const source = (await import('node:fs')).readFileSync(clientFile, 'utf8')
  assert.match(source, /api\/ai-prison\/status/)
  assert.match(source, /credentials: 'same-origin'/)
  assert.doesNotMatch(source, /document\.body\.appendChild/)
  assert.doesNotMatch(source, /<script/)
})
