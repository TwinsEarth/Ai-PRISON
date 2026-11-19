import fs from 'node:fs'
import { MAX_INCIDENTS } from './watchdog-state.js'
import { notify } from './notifier.js'

/**
 * Durable state of the watchdog: `state.json` for the cross-run snapshot,
 * `incidents.jsonl` for the append-only incident tail, and the restore path for
 * a previous run. All writes are best effort: a broken disk must not break the
 * watchdog's in-memory behavior.
 */
export function createPersistence({ watchdogState, stateFile, incidentsFile }) {
  let lastFlush = 0
  let tamperMigrationDone = false

  function persist(force = false) {
    const now = Date.now()
    if (!force && now - lastFlush < 30_000) return
    lastFlush = now
    const payload = {
      startedAt: watchdogState.startedAt,
      lastCrashScan: watchdogState.lastCrashScan,
      incidents: watchdogState.incidents.slice(-50),
      escalation: watchdogState.escalation,
      baseline: watchdogState.baseline,
      budgets: watchdogState.budgets,
      quarantine: [...watchdogState.quarantine].map(([target, entry]) => ({ target, ...entry })),
      rss: watchdogState.rss || {},
      rssAt: watchdogState.rssAt || null,
      rssError: watchdogState.rssError || null,
      rssStats: watchdogState.rssStats || null,
      rssServiceError: watchdogState.rssServiceError || null,
      rssAttempts: watchdogState.rssAttempts || 0,
      migratedTamperQuarantine: tamperMigrationDone,
    }
    try {
      // Atomic replace: a crash mid-write must never leave a half state.json.
      const tempFile = `${stateFile}.tmp`
      fs.writeFileSync(tempFile, `${JSON.stringify(payload, null, 2)}\n`)
      fs.renameSync(tempFile, stateFile)
    } catch {
      try {
        fs.rmSync(`${stateFile}.tmp`, { force: true })
      } catch {
        // best effort
      }
    }
  }

  function restoreState() {
    try {
      const parsed = JSON.parse(fs.readFileSync(stateFile, 'utf8'))
      watchdogState.lastCrashScan = Number(parsed.lastCrashScan) > 0 ? Number(parsed.lastCrashScan) : 0
      if (Array.isArray(parsed.incidents)) watchdogState.incidents = parsed.incidents.slice(-MAX_INCIDENTS)
      if (parsed.escalation && Array.isArray(parsed.escalation.crashes)) {
        watchdogState.escalation = { crashes: parsed.escalation.crashes.slice(-50), level: parsed.escalation.level || 'L0' }
      }
      if (parsed.baseline && Array.isArray(parsed.baseline.bundles)) watchdogState.baseline = parsed.baseline
      if (parsed.budgets && typeof parsed.budgets === 'object') watchdogState.budgets = parsed.budgets
      if (parsed.rss && typeof parsed.rss === 'object') watchdogState.rss = parsed.rss
      if (parsed.rssAt) watchdogState.rssAt = parsed.rssAt
      if (parsed.rssStats) watchdogState.rssStats = parsed.rssStats
      watchdogState.rssAttempts = Number(parsed.rssAttempts) || 0
      tamperMigrationDone = parsed.migratedTamperQuarantine === true
      if (Array.isArray(parsed.quarantine)) {
        let dropped = 0
        for (const entry of parsed.quarantine) {
          if (!tamperMigrationDone && entry && entry.reason === 'tamper') {
            dropped += 1
            continue
          }
          if (entry && typeof entry.target === 'string') {
            watchdogState.quarantine.set(entry.target, {
              reason: entry.reason || 'crash',
              level: entry.level || 'L1',
              at: entry.at || new Date().toISOString(),
              release: entry.release || 'manual',
              evidence: entry.evidence || '',
            })
          }
        }
        if (dropped > 0) {
          watchdogState.incidents.push({
            at: new Date().toISOString(),
            mode: watchdogState.mode,
            kind: 'migration',
            detail: '清理 ' + dropped + ' 条 1.4.3/1.4.4 误判的 tamper 隔离（操作窗口归因已修复）',
          })
        }
        tamperMigrationDone = true
      }
    } catch {
      // first run or unreadable state: start clean
    }
  }

  function appendIncident(incident) {
    const record = { at: new Date().toISOString(), mode: watchdogState.mode, ...incident }
    const notificationKinds = ['freeze', 'pause', 'quarantine', 'tamper', 'crash-enforced', 'crash-escalation', 'budget', 'quarantine-reminder']
    if ((record.notify === true || (watchdogState.config && watchdogState.config.notify === true)) && notificationKinds.includes(record.kind)) {
      try {
        notify(record)
      } catch {
        // notifications are best effort
      }
    }
    watchdogState.incidents.push(record)
    if (watchdogState.incidents.length > MAX_INCIDENTS) watchdogState.incidents = watchdogState.incidents.slice(-MAX_INCIDENTS)
    try {
      fs.appendFileSync(incidentsFile, `${JSON.stringify(record)}\n`)
      const stat = fs.statSync(incidentsFile)
      if (stat.size > 1024 * 1024) {
        // Keep a longer tail (2000 lines ≈ several days of typical incidents)
        // and write it atomically so a crash cannot truncate the audit file.
        const lines = fs.readFileSync(incidentsFile, 'utf8').trim().split('\n').slice(-2000)
        fs.writeFileSync(`${incidentsFile}.tmp`, `${lines.join('\n')}\n`)
        fs.renameSync(`${incidentsFile}.tmp`, incidentsFile)
      }
    } catch {
      // auditing is best effort
    }
    return record
  }

  return { persist, restoreState, appendIncident }
}
