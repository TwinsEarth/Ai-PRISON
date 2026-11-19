import v8 from 'node:v8'
import { MAX_SAMPLES } from './watchdog-state.js'
import { classifyWaterline, formatBytes, thresholdsFor } from './watchdog-policy.js'
import { remediationFor } from './remediation.js'

/**
 * Heap sampling and waterline transitions (ok -> warn -> pause -> frost).
 *
 * `sample()` records one metric sample, emits the first incident per level and
 * calls `onAfterSample` so the runtime can schedule the clean baseline and
 * refresh the tamper status. It never touches the plugin manager.
 */
export function createSampler({ watchdogState, persist, appendIncident, log, onAfterSample }) {
  function sample() {
    let heap
    try {
      heap = v8.getHeapStatistics()
    } catch {
      heap = { heap_size_limit: 0, used_heap_size: 0, total_heap_size: 0 }
    }
    const usage = process.memoryUsage()
    const heapLimit = Number(heap.heap_size_limit) > 0 ? Number(heap.heap_size_limit) : 4 * 1024 * 1024 * 1024
    const thresholds = thresholdsFor(heapLimit)
    watchdogState.thresholds = thresholds
    const heapUsed = Number(usage.heapUsed) > 0 ? Number(usage.heapUsed) : Number(heap.used_heap_size) || 0
    const previous = watchdogState.waterline
    const waterline = classifyWaterline(heapUsed, thresholds, previous)
    const record = {
      t: new Date().toISOString(),
      heapUsed,
      heapTotal: Number(usage.heapTotal) || Number(heap.total_heap_size) || 0,
      external: Number(usage.external) || 0,
      rss: Number(usage.rss) || 0,
      heapLimit,
      waterline,
    }
    watchdogState.latest = record
    watchdogState.waterline = waterline
    watchdogState.strongWarn = waterline !== 'ok' && heapUsed >= thresholds.opWarn
    watchdogState.samples.push(record)
    if (watchdogState.samples.length > MAX_SAMPLES) watchdogState.samples = watchdogState.samples.slice(-MAX_SAMPLES)

    if (waterline === 'frost' && !watchdogState.frozenAt) {
      watchdogState.frozenAt = record.t
      const incident = appendIncident({
        kind: 'freeze',
        level: 'frost',
        heapUsed,
        heapLimit,
        detail: `heap ${formatBytes(heapUsed)} ≥ block ${formatBytes(thresholds.totalBlock)}：冻结插件操作`,
        remediation: remediationFor('freeze'),
      })
      log.warn(`[ai-prison-watchdog] ${incident.detail}`)
      persist(true)
    } else if (waterline === 'pause' && !watchdogState.pausedAt) {
      watchdogState.pausedAt = record.t
      appendIncident({
        kind: 'pause',
        level: 'pause',
        heapUsed,
        heapLimit,
        detail: `heap ${formatBytes(heapUsed)} ≥ pause ${formatBytes(thresholds.totalPause)}：插件操作需二次确认`,
        remediation: remediationFor('pause'),
      })
      persist(true)
    } else if (waterline === 'warn' && previous !== 'warn' && previous === 'ok') {
      appendIncident({ kind: 'waterline', level: 'warn', heapUsed, heapLimit, detail: `heap ${formatBytes(heapUsed)} ≥ warn ${formatBytes(thresholds.totalWarn)}` })
    }
    if (waterline === 'ok') {
      watchdogState.pausedAt = null
      watchdogState.frozenAt = null
      watchdogState.graceUntil = null
    }
    if (waterline === 'warn') {
      watchdogState.frozenAt = null
      watchdogState.graceUntil = null
    }
    if (watchdogState.strongWarn && waterline === 'pause') {
      appendIncident({ kind: 'strong-warn', level: 'pause', heapUsed, heapLimit, detail: `heap ${formatBytes(heapUsed)} ≥ 强警告 ${formatBytes(thresholds.opWarn)}` })
    }
    if (typeof onAfterSample === 'function') onAfterSample()
  }

  return { sample }
}
