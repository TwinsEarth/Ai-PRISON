/**
 * Shared in-process state of the AI Prison watchdog. The watchdog loader row
 * writes it; the panel tool and the HTTP route read it. Same package, same Node
 * process, so one module instance is enough (no service plumbing needed).
 */
export const watchdogState = {
  mode: 'observe',
  startedAt: null,
  dataDir: null,
  config: {},
  /** Latest metrics sample. */
  latest: null,
  /** Ring of recent metrics samples (oldest first). */
  samples: [],
  thresholds: null,
  waterline: 'ok',
  /** target -> { count, estimateMB, lastAt, version, lastApplication } */
  operations: new Map(),
  /** Recent incidents (newest last), capped. */
  incidents: [],
  /** Last crash-log scan marker (ms). */
  lastCrashScan: 0,
  /** Non-fatal degradation reason, if any. */
  degraded: null,
}

export const MAX_SAMPLES = 120
export const MAX_INCIDENTS = 200

export function resetWatchdogState(overrides = {}) {
  watchdogState.mode = 'observe'
  watchdogState.startedAt = null
  watchdogState.dataDir = null
  watchdogState.config = {}
  watchdogState.latest = null
  watchdogState.samples = []
  watchdogState.thresholds = null
  watchdogState.waterline = 'ok'
  watchdogState.operations = new Map()
  watchdogState.incidents = []
  watchdogState.lastCrashScan = 0
  watchdogState.degraded = null
  Object.assign(watchdogState, overrides)
  return watchdogState
}
