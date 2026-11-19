import { createWatchdog } from './lib/watchdog-runtime.js'

/**
 * AI Prison watchdog loader row.
 *
 * Samples the host heap, accounts plugin operations, reads recent host crash
 * logs, watches the profile files and startup memory, and serves the status and
 * action routes under /api/ai-prison/watchdog/*.
 *
 * Enforcement is on by default (`enforce: true`): at the pause/freeze waterlines
 * it can roll back additive operations and, at the hot-operation limit, close
 * and quarantine the offending plugin. Set `config.enforce: false` to run in
 * observe mode, where everything is recorded but no plugin is ever touched.
 * Capability-removal backends of the AI Prison prototype remain simulated; this
 * row only governs DSH plugin operations and memory pressure.
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
