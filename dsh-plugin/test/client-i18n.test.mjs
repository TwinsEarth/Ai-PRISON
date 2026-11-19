import assert from 'node:assert/strict'
import fs from 'node:fs'
import path from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const source = fs.readFileSync(path.join(root, 'client.js'), 'utf8')

/** Extract the object literal of one inline dictionary (`en: {`/`zh: {`). */
function dictionaryBody(language) {
  const start = source.indexOf(`${language}: {`)
  assert.ok(start >= 0, `${language} dictionary exists`)
  const open = source.indexOf('{', start)
  let depth = 0
  for (let index = open; index < source.length; index += 1) {
    const char = source[index]
    if (char === '{') depth += 1
    else if (char === '}') {
      depth -= 1
      if (depth === 0) return source.slice(open, index + 1)
    }
  }
  throw new Error(`${language} dictionary is unterminated`)
}

function keysOf(language) {
  const body = dictionaryBody(language)
  const keys = [...body.matchAll(/'([a-zA-Z0-9_.]+)':\s*'/g)].map((match) => match[1])
  return { keys, duplicates: keys.filter((key, index) => keys.indexOf(key) !== index) }
}

/** Keys passed to t() as literals, plus the two dynamic idle labels from labelOf(). */
function usedKeys() {
  const keys = new Set([...source.matchAll(/\bt\('([a-zA-Z0-9_.]+)'/g)].map((match) => match[1]))
  for (const match of source.matchAll(/labelOf\('([a-z-]+)',\s*'([a-zA-Z0-9_.]+)'\)/g)) keys.add(match[2])
  return [...keys]
}

test('every translation key used by the panel exists in both inline dictionaries', () => {
  const en = keysOf('en')
  const zh = keysOf('zh')
  const used = usedKeys()
  assert.ok(used.length >= 70, `client uses ${used.length} translation keys`)
  for (const key of used) {
    assert.ok(en.keys.includes(key), `en dictionary is missing ${key}`)
    assert.ok(zh.keys.includes(key), `zh dictionary is missing ${key}`)
  }
  assert.deepEqual(en.duplicates, [], 'en dictionary has no duplicate keys')
  assert.deepEqual(zh.duplicates, [], 'zh dictionary has no duplicate keys')
})

test('both dictionaries cover the same key set (no locale-only drift)', () => {
  const en = new Set(keysOf('en').keys)
  const zh = new Set(keysOf('zh').keys)
  const onlyEn = [...en].filter((key) => !zh.has(key))
  const onlyZh = [...zh].filter((key) => !en.has(key))
  assert.deepEqual(onlyEn, [], 'en-only keys')
  assert.deepEqual(onlyZh, [], 'zh-only keys')
})

test('the enforce and prison cards no longer fall back to raw keys', () => {
  for (const key of [
    'watchdog.enforceTitle',
    'watchdog.confirmPause',
    'watchdog.quarantine',
    'watchdog.release',
    'watchdog.topFive',
    'watchdog.heap6gOff',
    'prison.plugin',
    'prison.rss',
  ]) {
    const body = dictionaryBody('en')
    assert.ok(body.includes(`'${key}'`), `en has ${key}`)
    assert.ok(dictionaryBody('zh').includes(`'${key}'`), `zh has ${key}`)
  }
})

test('memory tier labels exist in both dictionaries', () => {
  for (const key of ['memoryTier.ok', 'memoryTier.warn', 'memoryTier.throttle', 'memoryTier.close', 'memoryTier.uninstall', 'memoryTier.all-third-party']) {
    assert.ok(dictionaryBody('en').includes(`'${key}'`), `en has ${key}`)
    assert.ok(dictionaryBody('zh').includes(`'${key}'`), `zh has ${key}`)
  }
})

test('memory waterline band labels exist in both dictionaries', () => {
  for (const key of ['waterline.easy', 'waterline.normal', 'waterline.warn', 'waterline.danger']) {
    assert.ok(dictionaryBody('en').includes(`'${key}'`), `en has ${key}`)
    assert.ok(dictionaryBody('zh').includes(`'${key}'`), `zh has ${key}`)
  }
})

test('locale JSON carries market metadata only; runtime strings have one source', () => {
  for (const language of ['en', 'zh']) {
    const json = JSON.parse(fs.readFileSync(path.join(root, 'locale', `${language}.json`), 'utf8'))
    assert.deepEqual(Object.keys(json).sort(), ['meta'], `${language}.json only carries the market metadata`)
    assert.ok(json.meta.title && json.meta.description && json.meta.detail)
  }
  // Every runtime key lives in the inline client dictionaries; the locale JSON
  // must not grow a second runtime dictionary again.
  const dictKeys = new Set([...keysOf('en').keys])
  assert.ok(dictKeys.has('watchdog.enforceTitle'))
  assert.ok(dictKeys.has('action.runDemo') || source.includes("labelOf('demo', 'action.runDemo')"))
})
