import { probeBudget } from './budget-probe.js'
import { remediationFor } from './remediation.js'
import { prunePluginState } from './watchdog-policy.js'
import { readJournalRecords } from './watchdog-core.js'

/**
 * Per-plugin startup budget probes.
 *
 * A candidate is a non-exempt enabled bundle whose measured version is missing,
 * stale, errored or older than the retry window. Probes run in a child process
 * (`budget-probe.js` -> `budget-child.mjs`), one at a time, and only while the
 * waterline is OK. A probe above the singleBlock threshold quarantines the
 * plugin and writes a pending-boot plan.
 */
export function createBudget({ watchdogState, refs, profileDir, journalDir, exemptions, appendIncident, persist, enforcement, config }) {
  const probeFn = typeof config.probe === 'function' ? config.probe : probeBudget
  const probeMax = Number(config.probeMax) > 0 ? Number(config.probeMax) : 5
  const probeRetryMs = Number(config.probeRetryMs) > 0 ? Number(config.probeRetryMs) : 10 * 60 * 1000
  const probeMaxAgeMs = 7 * 24 * 3600 * 1000

  function budgetState(name, version) {
    const entry = watchdogState.budgets[name]
    if (!entry) return 'missing'
    if (entry.error) {
      const age = Date.now() - Date.parse(entry.measuredAt || 0)
      return age > probeRetryMs && (Number(entry.attempts) || 0) < 5 ? 'retry' : 'error'
    }
    if (entry.moduleLoadMB === undefined) return 'stale'
    if (!entry.version || (version && entry.version !== version)) return 'stale'
    if (Date.now() - Date.parse(entry.measuredAt || 0) > probeMaxAgeMs) return 'stale'
    return 'ok'
  }

  async function probeOne(name, version) {
    const previous = watchdogState.budgets[name] || {}
    watchdogState.probeProgress = { ...(watchdogState.probeProgress || { done: 0, total: 0 }), current: name }
    const result = await probeFn({ profileDir, packageName: name })
    if (!result || !result.ok) {
      watchdogState.budgets[name] = {
        error: String((result && result.error) || 'probe-failed').slice(0, 300),
        measuredAt: new Date().toISOString(),
        version: version || previous.version || null,
        attempts: (Number(previous.attempts) || 0) + 1,
        source: 'module-load-probe',
      }
      persist(true)
      return { ok: false }
    }
    watchdogState.budgets[name] = {
      moduleLoadMB: result.moduleLoadMB,
      rssMB: result.rssMB,
      ms: result.ms,
      measuredAt: new Date().toISOString(),
      version: version || null,
      attempts: (Number(previous.attempts) || 0) + 1,
      source: 'module-load-probe',
    }
    const thresholds = watchdogState.thresholds
    const mb = result.moduleLoadMB
    // Thresholds are clamped to the real heap cap, so a small-heap host has a
    // lower absolute limit than the documented 300/500 MB. Report the value
    // that actually applied instead of the documentation default.
    const blockMB = thresholds ? Math.round(thresholds.singleBlock / (1024 * 1024)) : 500
    const warnMB = thresholds ? Math.round(thresholds.singleWarn / (1024 * 1024)) : 300
    if (thresholds && mb >= blockMB) {
      appendIncident({ kind: 'plugin-budget', target: name, level: 'block', detail: '启动实测 ' + mb + ' MB > ' + blockMB + ' MB，禁止该插件' })
      if (watchdogState.enforce) {
        watchdogState.quarantine.set(name, { reason: 'budget', level: 'L1', at: new Date().toISOString(), release: 'manual', evidence: mb + ' MB > ' + blockMB + ' MB', remediation: remediationFor('budget') })
        void enforcement.disableTarget(name, 'budget-over-500MB')
        enforcement.writePendingBoot({ level: 'L1', reason: 'budget', disable: [name], keep: [] })
      }
    } else if (thresholds && mb >= warnMB) {
      appendIncident({ kind: 'plugin-budget', target: name, level: 'warn', detail: '启动实测 ' + mb + ' MB > ' + warnMB + ' MB，仅提醒' })
    }
    persist(true)
    return { ok: true }
  }

  function scheduleProbe({ force = false } = {}) {
    if (watchdogState.probeRunning) return Promise.resolve(watchdogState.probeProgress)
    const manager = refs.manager
    if (!manager || typeof manager.listBundles !== 'function') return Promise.resolve(null)
    // Claim the run synchronously, before the first await: two callers in the
    // same tick must not both pass the flag check and start a probe.
    watchdogState.probeRunning = true
    const token = (scheduleProbe.generation = (scheduleProbe.generation || 0) + 1)
    return (async () => {
      try {
        let rows
        try {
          rows = await manager.listBundles()
        } catch {
          return null
        }
        prunePluginState(watchdogState, rows)
        const recent = new Set()
        const cutoff = Date.now() - 30 * 60 * 1000
        for (const record of readJournalRecords(journalDir)) {
          const operation = record && record.operation
          if (!operation || !operation.target) continue
          if (operation.stage !== 'install' && operation.stage !== 'enable') continue
          if (Date.parse(operation.time || 0) < cutoff) continue
          recent.add(operation.target)
        }
        const candidates = rows
          .filter((row) => row.enabled && !exemptions.has(row.name))
          .map((row) => ({ name: row.name, version: row.version ? String(row.version) : '', state: budgetState(row.name, row.version ? String(row.version) : '') }))
          .filter((candidate) => candidate.state !== 'ok')
          .sort((a, b) => (recent.has(b.name) ? 1 : 0) - (recent.has(a.name) ? 1 : 0))
          .slice(0, force ? Math.max(probeMax, Number(config.probeMaxForce) > 0 ? Number(config.probeMaxForce) : 15) : probeMax)
        watchdogState.probeProgress = { done: 0, total: candidates.length, current: null, finishedAt: null }
        if (candidates.length === 0) {
          watchdogState.probeProgress.finishedAt = new Date().toISOString()
          return watchdogState.probeProgress
        }
        for (const candidate of candidates) {
          if (token !== scheduleProbe.generation || watchdogState.waterline !== 'ok') break
          let outcome
          try {
            outcome = await probeOne(candidate.name, candidate.version)
          } catch (error) {
            watchdogState.budgets[candidate.name] = {
              error: String((error && error.message) || error).slice(0, 300),
              measuredAt: new Date().toISOString(),
              version: candidate.version || null,
              attempts: 1,
              source: 'module-load-probe',
            }
            persist(true)
            outcome = { ok: false }
          }
          watchdogState.probeProgress = { ...watchdogState.probeProgress, done: watchdogState.probeProgress.done + 1, current: candidate.name, lastOk: Boolean(outcome && outcome.ok) }
          if (candidates.length > 1) await new Promise((resolve) => setTimeout(resolve, Number(config.probeGapMs) >= 0 ? Number(config.probeGapMs) : 1500))
        }
        return watchdogState.probeProgress
      } finally {
        if (token === scheduleProbe.generation) {
          watchdogState.probeRunning = false
          watchdogState.probeProgress = { ...(watchdogState.probeProgress || { done: 0, total: 0 }), current: null, finishedAt: new Date().toISOString() }
          persist(true)
        }
      }
    })()
  }

  return { scheduleProbe }
}
