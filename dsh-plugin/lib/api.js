import { resolveCli, resolveDataDir, runCli } from './cli.js'
import { listSegments, resetLedger } from './ledger.js'
import { buildStatus, summarizeRun } from './status.js'

/**
 * Resolve the directory panel actions should default to. The route has no Agent
 * of its own, so it borrows the most recent live Session's effective directory;
 * without one it falls back to the Host process directory.
 */
export function resolveSessionCwd(ctx) {
  try {
    const sessions = ctx && typeof ctx.get === 'function' ? ctx.get('sessions') : undefined
    const workingDirectory = ctx && typeof ctx.get === 'function' ? ctx.get('workingDirectory') : undefined
    const list = sessions && typeof sessions.list === 'function' ? sessions.list() : []
    for (let index = list.length - 1; index >= 0; index -= 1) {
      const session = list[index]
      if (workingDirectory && typeof workingDirectory.get === 'function') {
        try {
          const dir = workingDirectory.get(session)
          if (typeof dir === 'string' && dir) return dir
        } catch {
          // A session whose directory is unavailable falls through to its header.
        }
      }
      const headerCwd = session && session.header ? session.header.cwd : undefined
      if (typeof headerCwd === 'string' && headerCwd) return headerCwd
    }
  } catch {
    // A profile without sessions keeps the process directory.
  }
  return undefined
}

const ACTIONS = new Set(['demo', 'drill', 'reset', 'reset-demo'])

function jsonResponse(payload, status = 200) {
  return Response.json(payload, { status, headers: { 'cache-control': 'no-store' } })
}

/**
 * Panel API: read-only status plus the explicit ledger actions the panel offers.
 * `demo` needs an empty ledger (the CLI replays an existing one and refuses to
 * re-seed revoked credentials), so the panel offers `reset-demo` to rebuild it.
 */
export function registerApi({ ctx, config = {}, logger } = {}) {
  if (!ctx || typeof ctx.inject !== 'function') return false
  ctx.inject(['connection'], (child) => {
    const registerRoute = (route, label) => {
      try {
        child.effect(() => child.connection.fetch.register(route), label)
      } catch (error) {
        logger?.warn?.(`注册 ${route.path} 失败：${String(error && error.message)}`)
      }
    }

    registerRoute(
      {
        path: '/api/ai-prison/status',
        methods: ['GET'],
        requestBody: 'buffered',
        fetch: async (request) => {
          try {
            const url = new URL(request.url)
            const requested = url.searchParams.get('data_dir')
            const statusConfig = requested ? { ...config, dataDir: requested } : config
            const status = await buildStatus({ config: statusConfig, cwd: resolveSessionCwd(ctx) })
            return jsonResponse(status)
          } catch (error) {
            return jsonResponse({ ok: false, error: String((error && error.message) || error) }, 500)
          }
        },
      },
      'ai-prison: status api',
    )

    registerRoute(
      {
        path: '/api/ai-prison/run',
        methods: ['POST'],
        requestBody: 'buffered',
        fetch: async (request) => {
          let body
          try {
            body = await request.json()
          } catch {
            body = {}
          }
          const action = String(body?.action ?? '')
          if (!ACTIONS.has(action)) {
            return jsonResponse({ ok: false, error: 'action must be one of demo, drill, reset, reset-demo' }, 400)
          }
          const resolved = resolveCli({ config })
          const cwd = resolveSessionCwd(ctx) || process.cwd()
          const requested = typeof body?.data_dir === 'string' && body.data_dir ? body.data_dir : undefined
          const dataDir = requested ? requested : resolveDataDir({ config, cwd })

          let removed = []
          if (action === 'reset' || action === 'reset-demo') {
            const reset = resetLedger(dataDir)
            if (!reset.ok) {
              return jsonResponse({ ok: false, action, dataDir, error: reset.error || 'reset failed' }, 500)
            }
            removed = reset.removed
            if (action === 'reset') {
              return jsonResponse({
                ok: true,
                action,
                dataDir,
                removed,
                summary: `已清空 ${removed.length} 个账本段`,
              })
            }
          }

          if (!resolved.path) {
            return jsonResponse({ ok: false, error: resolved.missing, reason: 'cli-missing' }, 409)
          }
          const timeoutMs = Number(config.timeoutMs) > 0 ? Number(config.timeoutMs) : 120_000
          const runAction = action === 'reset-demo' ? 'demo' : action
          if (runAction === 'demo' && listSegments(dataDir).length > 0) {
            return jsonResponse(
              {
                ok: false,
                action,
                dataDir,
                error: 'ledger-not-empty',
                hint: '账本已有记录：demo 需要空账本。请点「重置账本并重建」，或改用「运行恢复演练」。',
              },
              409,
            )
          }
          const result = await runCli(['--data-dir', dataDir, runAction], {
            bin: resolved.path,
            cwd,
            timeoutMs: Math.max(timeoutMs, 180_000),
          })
          return jsonResponse({
            ok: result.ok,
            action,
            dataDir,
            cwd,
            removed,
            exitCode: result.exitCode,
            timedOut: result.timedOut === true,
            summary: summarizeRun(runAction, result),
            stdout: result.stdout,
            stderr: result.stderr,
          })
        },
      },
      'ai-prison: run api',
    )
  })
  return true
}
