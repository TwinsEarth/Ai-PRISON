/**
 * Shared in-process state of the AI Prison watchdog.
 *
 * The shape started as a P0 observe-only snapshot; it now also holds the
 * enforcement registries (quarantine, pause grace, hot-operation counters,
 * budget table, baseline, crash plans). Enforcement is the default; observe
 * mode (`config.enforce: false`) uses the same state and only skips actions.
 * Nothing here touches the plugin manager by itself.
 */
export const watchdogState = {
  mode: 'enforce',
  enforce: true,
  startedAt: null,
  runId: null,
  dataDir: null,
  config: {},
  /** Latest metrics sample. */
  latest: null,
  /** Ring of recent metrics samples (oldest first). */
  samples: [],
  thresholds: null,
  waterline: 'ok',
  strongWarn: false,
  pausedAt: null,
  graceUntil: null,
  frozenAt: null,
  /** target -> { count, estimateMB, lastAt, version, lastApplication } */
  operations: new Map(),
  /** target -> { tier, mb, at, version } from the plugin-prison memory thresholds. */
  memoryTiers: new Map(),
  /** target -> { reason, level, at, evidence, release: 'restart'|'manual' } */
  quarantine: new Map(),
  /** target -> ms timestamp until which operation events are our own doing. */
  suppressUntil: new Map(),
  /** name@version -> measured/estimated startup MB (budget probe feeds this in P2). */
  budgets: {},
  /** Last clean baseline: { at, runId, bundles: [{name, version, enabled}], files } */
  baseline: null,
  /** Crash attribution plans (dry-run): { at, level, target, evidence, applied } */
  plans: [],
  /** Escalation counters: crash timestamps (ms) and applied level. */
  escalation: { crashes: [], level: 'L0', previousLevel: null, lastCrashAt: null },
  /** Profile file tamper monitor status. */
  tamper: null,
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
  watchdogState.mode = 'enforce'
  watchdogState.enforce = true
  watchdogState.startedAt = null
  watchdogState.runId = null
  watchdogState.dataDir = null
  watchdogState.config = {}
  watchdogState.latest = null
  watchdogState.samples = []
  watchdogState.thresholds = null
  watchdogState.waterline = 'ok'
  watchdogState.strongWarn = false
  watchdogState.pausedAt = null
  watchdogState.graceUntil = null
  watchdogState.frozenAt = null
  watchdogState.operations = new Map()
  watchdogState.memoryTiers = new Map()
  watchdogState.quarantine = new Map()
  watchdogState.suppressUntil = new Map()
  watchdogState.budgets = {}
  watchdogState.baseline = null
  watchdogState.plans = []
  watchdogState.escalation = { crashes: [], level: 'L0', previousLevel: null, lastCrashAt: null }
  watchdogState.tamper = null
  watchdogState.incidents = []
  watchdogState.lastCrashScan = 0
  watchdogState.degraded = null
  Object.assign(watchdogState, overrides)
  return watchdogState
}
