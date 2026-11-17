import { buildStatus } from './status.js'

/**
 * Read-only status API behind the sidebar panel. It registers through
 * `ctx.connection.fetch`, so the route is authenticated exactly like the rest of
 * the Web UI and needs no separate web server.
 */
export function registerApi({ ctx, config = {}, logger } = {}) {
  if (!ctx || typeof ctx.inject !== 'function') return false
  ctx.inject(['connection'], (child) => {
    try {
      child.effect(
        () =>
          child.connection.fetch.register({
            path: '/api/ai-prison/status',
            methods: ['GET'],
            requestBody: 'buffered',
            fetch: async (request) => {
              try {
                const url = new URL(request.url)
                const requested = url.searchParams.get('data_dir')
                const statusConfig = requested ? { ...config, dataDir: requested } : config
                const status = await buildStatus({ config: statusConfig })
                return Response.json(status, { headers: { 'cache-control': 'no-store' } })
              } catch (error) {
                return Response.json(
                  { ok: false, error: String((error && error.message) || error) },
                  { status: 500 },
                )
              }
            },
          }),
        'ai-prison: status api',
      )
    } catch (error) {
      logger?.warn?.(`注册 /api/ai-prison/status 失败：${String(error && error.message)}`)
    }
  })
  return true
}
