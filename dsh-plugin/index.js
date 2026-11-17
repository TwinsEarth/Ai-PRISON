import { registerApi } from './lib/api.js'
import { installTools } from './lib/tools.js'

function loggerOf(ctx) {
  const logger = ctx && typeof ctx === 'object' ? ctx.logger : undefined
  return {
    info: (...args) => (typeof logger?.info === 'function' ? logger.info(...args) : console.log('[ai-prison]', ...args)),
    warn: (...args) => (typeof logger?.warn === 'function' ? logger.warn(...args) : console.warn('[ai-prison]', ...args)),
  }
}

/**
 * Host half of the bundle. It registers the aiprison_* tools and the read-only
 * status API; the Client half (client.js) owns the sidebar panel. Both halves
 * are optional services: a profile without `connection` keeps the tools, and a
 * harness whose tools module cannot be resolved keeps the panel.
 */
export function apply(ctx, config = {}) {
  const logger = loggerOf(ctx)
  const pluginConfig = config && typeof config === 'object' ? config : {}

  const apiInstalled = registerApi({ ctx, config: pluginConfig, logger })
  if (!apiInstalled) logger.info('ctx.inject 不可用：状态 API 未注册')

  installTools({ ctx, config: pluginConfig, logger }).catch((error) => {
    logger.warn(`aiprison_* 工具注册失败：${String((error && error.message) || error)}`)
  })

  ctx.effect(() => () => logger.info('AI Prison 插件已卸载'), 'ai-prison: lifecycle')
}
