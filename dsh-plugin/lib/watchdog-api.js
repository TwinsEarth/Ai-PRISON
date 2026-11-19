import fs from 'node:fs'
import path from 'node:path'
import { PAUSE_GRACE_MS } from './watchdog-core.js'

/**
 * HTTP surface of the watchdog: the read-only status snapshot and the explicit
 * panel actions (probe, release, close, confirm-pause, 6 GB heap request).
 *
 * Registered through `ctx.inject(['connection'])`; every action is driven by the
 * panel and the response is JSON with `cache-control: no-store`.
 */
export function registerWatchdogApi({ ctx, watchdogState, getSnapshot, actions, logger }) {
  try {
    if (typeof ctx.inject !== 'function') return false
    ctx.inject(['connection'], (child) => {
      try {
        child.effect(
          () =>
            child.connection.fetch.register({
              path: '/api/ai-prison/watchdog/status',
              methods: ['GET'],
              requestBody: 'buffered',
              fetch: async () => Response.json(getSnapshot(), { headers: { 'cache-control': 'no-store' } }),
            }),
          'ai-prison-watchdog: status api',
        )
        child.effect(
          () =>
            child.connection.fetch.register({
              path: '/api/ai-prison/watchdog/action',
              methods: ['POST'],
              requestBody: 'buffered',
              fetch: async (request) => {
                let body = {}
                try {
                  body = await request.json()
                } catch {
                  body = {}
                }
                const action = typeof body.action === 'string' ? body.action : ''
                const target = typeof body.target === 'string' ? body.target : ''
                if (action === 'heap6g') {
                  const enabled = body.enabled === true
                  // The request lives inside the watchdog state directory; an
                  // external launcher is still required to act on it, and none
                  // ships with the plugin. Never write outside the profile.
                  const flagPath = path.join(watchdogState.dataDir || '', 'heap6g-request.json')
                  try {
                    if (enabled) fs.writeFileSync(flagPath, JSON.stringify({ enabled: true, at: new Date().toISOString(), consumer: 'external-launcher-required' }, null, 2) + '\n')
                    else fs.rmSync(flagPath, { force: true })
                    watchdogState.heap6g = enabled
                    actions.appendIncident({ kind: 'heap6g', detail: enabled ? '已记录 6GB 堆请求（需外部启动器消费；当前未安装）' : '已取消 6GB 堆请求' })
                  } catch (error) {
                    return Response.json({ ok: false, error: String((error && error.message) || error) }, { status: 500 })
                  }
                  return Response.json({ ok: true, enabled, consumer: 'external-launcher-required', requestFile: flagPath })
                }
                if (action === 'probe') {
                  const progress = await actions.scheduleProbe({ force: true })
                  return Response.json({ ok: true, progress })
                }
                if (action === 'confirm-pause') {
                  watchdogState.graceUntil = Date.now() + PAUSE_GRACE_MS
                  actions.appendIncident({ kind: 'pause-confirmed', detail: '用户二次确认继续插件操作（宽限 10 分钟）' })
                  return Response.json({ ok: true })
                }
                if (action === 'release') return Response.json(actions.releaseTarget(target))
                if (action === 'close') {
                  const ok = await actions.disableTarget(target, 'manual-close')
                  return Response.json({ ok })
                }
                return Response.json({ ok: false, error: 'unknown action' }, { status: 400 })
              },
            }),
          'ai-prison-watchdog: action api',
        )
      } catch (error) {
        if (logger && typeof logger.warn === 'function') logger.warn('[ai-prison-watchdog] status api: ' + String((error && error.message) || error))
      }
    })
    return true
  } catch (error) {
    if (logger && typeof logger.warn === 'function') logger.warn('[ai-prison-watchdog] connection inject: ' + String((error && error.message) || error))
    return false
  }
}
