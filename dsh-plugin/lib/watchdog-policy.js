/**
 * Pure policy math for the AI Prison watchdog (P0 observe mode).
 * No I/O here: every function takes plain numbers/strings so the panel, the
 * tools and the tests share one definition of the thresholds.
 */
export const MB = 1024 * 1024
export const GB = 1024 * MB

/** Thresholds derived from the host heap limit (absolute values clamped to the cap). */
export function thresholdsFor(heapLimitBytes) {
  const limit = Number(heapLimitBytes) > 0 ? Number(heapLimitBytes) : 4 * GB
  const clamp = (absolute, ratio) => Math.min(absolute, ratio * limit)
  return {
    heapLimit: limit,
    singleWarn: clamp(300 * MB, 0.075),
    singleBlock: clamp(500 * MB, 0.125),
    totalWarn: clamp(3 * GB, 0.73),
    totalPause: clamp(3.9 * GB, 0.88),
    totalBlock: clamp(5 * GB, 0.93),
    opWarn: clamp(4 * GB, 2 / 3),
    opBlock: clamp(5 * GB, 5 / 6),
  }
}

/** Waterline for the whole host: ok | warn | pause | block. */
export function classifyWaterline(heapUsed, thresholds) {
  if (!thresholds || !(heapUsed >= 0)) return 'ok'
  if (heapUsed >= thresholds.totalBlock) return 'block'
  if (heapUsed >= thresholds.totalPause) return 'pause'
  if (heapUsed >= thresholds.totalWarn) return 'warn'
  return 'ok'
}

/** How much one plugin operation counts toward the per-target hot-operation limit. */
export function operationWeight(operation) {
  if (!operation || typeof operation !== 'object') return 0
  const { stage, enabled } = operation
  if (stage === 'install' || stage === 'remove') return 1
  if (stage === 'enable') return enabled === false ? 0.5 : 1
  return 0
}

/** Exponentially weighted estimate of a plugin's heap cost. */
export function mergeEstimate(previous, next, alpha = 0.5) {
  const value = Number(next)
  if (!Number.isFinite(value) || value < 0) return Number(previous) || 0
  const before = Number(previous)
  if (!Number.isFinite(before) || before <= 0) return value
  return Math.round(before * (1 - alpha) + value * alpha)
}

/** Parse the header block of a DSH desktop host crash log. */
export function parseCrashLog(text) {
  const source = String(text || '')
  const header = source.slice(0, 4000)
  const time = /^time:\s*(.+)$/m.exec(header)
  const phase = /^phase:\s*(.+)$/m.exec(header)
  const exit = /exited with (\d+)/.exec(source)
  const oom = /heap out of memory|Allocation failed|MarkCompactCollector|Scavenger:/i.test(source)
  const fatal = /FATAL ERROR/.test(source)
  return {
    valid: Boolean(time || exit || fatal),
    time: time ? time[1].trim() : null,
    phase: phase ? phase[1].trim() : null,
    hostExitCode: exit ? Number(exit[1]) : null,
    oom,
  }
}

/** A compact top-N view of the per-target accounting, biggest estimate first. */
export function topOperations(operations, limit = 5) {
  const rows = []
  for (const [target, entry] of operations instanceof Map ? operations : Object.entries(operations || {})) {
    rows.push({
      target,
      count: Number(entry.count) || 0,
      estimateMB: Number(entry.estimateMB) || 0,
      lastAt: entry.lastAt || null,
      version: entry.version || null,
    })
  }
  rows.sort((a, b) => b.estimateMB - a.estimateMB || b.count - a.count)
  return rows.slice(0, Math.max(1, limit))
}

/** Named byte formatting used by the panel/tool text. */
export function formatBytes(bytes) {
  const value = Number(bytes)
  if (!Number.isFinite(value) || value < 0) return '—'
  if (value >= GB) return `${(value / GB).toFixed(2)} GB`
  return `${Math.round(value / MB)} MB`
}
