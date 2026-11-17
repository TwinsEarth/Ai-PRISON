import { createWatchdog } from './lib/watchdog-runtime.js'

/**
 * AI Prison watchdog loader row (P0: observe only).
 *
 * It samples the host heap, accounts plugin operations, reads recent host crash
 * logs and serves a read-only snapshot at /api/ai-prison/watchdog/status.
 * It never enables or disables a plugin: enforcement stays in later phases until
 * the design is confirmed.
 */
export function apply(ctx, config = {}) {
  const logger = ctx && typeof ctx === 'object' ? ctx.logger : undefined
  try {
    createWatchdog(ctx, config, logger || {})
  } catch (error) {
    const message = String((error && error.stack) || error)
    if (logger && typeof logger.warn === 'function') logger.warn(`[ai-prison-watchdog] 启动失败：${message}`)
    else console.warn('[ai-prison-watchdog] 启动失败', message)
  }
}
