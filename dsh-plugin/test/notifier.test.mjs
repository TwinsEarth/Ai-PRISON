import assert from 'node:assert/strict'
import test from 'node:test'
import { notificationFor, resetNotifierState, shouldNotify } from '../lib/notifier.js'

test('enforcement incidents map to a notification title and body', () => {
  const message = notificationFor({ kind: 'freeze', detail: 'heap 5.1 GB >= 5.0 GB', target: null })
  assert.ok(message)
  assert.match(message.title, /内存冻结/)
  assert.match(message.body, /5\.1 GB/)
  assert.equal(notificationFor({ kind: 'baseline', detail: 'x' }), null)
})

test('the notifier is rate limited per kind', () => {
  resetNotifierState()
  const now = 1_000_000
  assert.equal(shouldNotify('freeze', now), true)
  assert.equal(shouldNotify('freeze', now + 10_000), false)
  assert.equal(shouldNotify('quarantine', now + 10_000), true)
  assert.equal(shouldNotify('freeze', now + 61_000), true)
  resetNotifierState()
})

test('the quarantine reminder has a notification title and body', async () => {
  const { notificationFor, shouldNotify, resetNotifierState } = await import('../lib/notifier.js')
  const message = notificationFor({ kind: 'quarantine-reminder', target: '@x/y', detail: '请到 AI 监狱释放该插件！再开启。' })
  assert.ok(message)
  assert.match(message.title, /隔离/)
  assert.match(message.body, /假释|释放/)
  resetNotifierState()
  assert.equal(shouldNotify('quarantine-reminder'), true)
})
