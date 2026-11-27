import assert from 'node:assert/strict'
import path from 'node:path'
import test from 'node:test'
import { installTools } from '../lib/tools.js'

function makeCtx(cwd) {
  const definitions = []
  const ctx = {
    get(key) {
      if (key === 'sessions') return { get: () => ({ header: { cwd } }) }
      return undefined
    },
    inject(deps, callback) {
      if (!deps.includes('tools')) return
      callback({
        tools: {
          register(definition) {
            definitions.push(definition)
            return definition
          },
        },
        effect: (fn) => fn(),
      })
    },
  }
  return { ctx, definitions }
}

test('agent tools reject a data_dir outside the shared whitelist', async () => {
  const cwd = path.join(process.cwd(), '.tmp-tool-guard')
  const { ctx, definitions } = makeCtx(cwd)
  await installTools({ ctx, config: {}, logger: { warn() {}, info() {} } })
  const byName = Object.fromEntries(definitions.map((definition) => [definition.name, definition]))
  assert.ok(byName.ai_prison_status)
  assert.ok(byName.ai_prison_demo)
  assert.deepEqual(byName.ai_prison_levels.parameters, {}, 'levels must not advertise an unused data_dir')

  for (const name of ['ai_prison_status', 'ai_prison_demo', 'ai_prison_inspect', 'ai_prison_verify', 'ai_prison_repair_tail', 'ai_prison_watchdog_bridge']) {
    const result = JSON.parse(await byName[name].execute({ data_dir: '/etc' }, { agent: { id: 'a' } }))
    assert.equal(result.ok, false, name)
    assert.equal(result.reason, 'data-dir-not-allowed', name)
  }
})

test('ledger-writing tools require an explicit confirmation', async () => {
  const cwd = path.join(process.cwd(), '.tmp-tool-confirm')
  const { ctx, definitions } = makeCtx(cwd)
  await installTools({ ctx, config: {}, logger: { warn() {}, info() {} } })
  const byName = Object.fromEntries(definitions.map((definition) => [definition.name, definition]))
  for (const name of ['ai_prison_demo', 'ai_prison_drill', 'ai_prison_repair_tail', 'ai_prison_watchdog_bridge']) {
    const result = JSON.parse(await byName[name].execute({}, { agent: { id: 'a' } }))
    assert.equal(result.ok, false, name)
    assert.equal(result.reason, 'confirmation-required', name)
  }
})
