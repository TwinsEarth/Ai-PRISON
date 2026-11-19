import fs from 'node:fs'
import path from 'node:path'
import { attributeCrash, diffBaseline, escalationLevel, parseCrashLog } from './watchdog-policy.js'
import { remediationFor } from './remediation.js'
import { CRASH_HEAD_BYTES, CRASH_WINDOW_MS, readHead, readJournalRecords } from './watchdog-core.js'

/**
 * Crash-log ingestion and graded crash plans.
 *
 * Every new `crash-*-host.log` is parsed, attributed to the last changed plugin
 * of that host run and turned into an L1-L3 plan. In enforce mode the plan is
 * executed through the enforcement module (close/quarantine + pending-boot).
 */
export function createCrash({ watchdogState, logsDir, journalDir, refs, exemptions, appendIncident, persist, enforcement }) {
  async function enforceCrashPlan(plan) {
    if (!plan || !plan.target || watchdogState.crashFallback !== 'enforce') return
    try {
      watchdogState.quarantine.set(plan.target, { reason: 'crash', level: plan.level, at: new Date().toISOString(), release: 'manual', evidence: plan.reason, remediation: remediationFor('crash') })
      const disabled = [plan.target].filter((target) => !exemptions.has(target))
      await enforcement.disableTarget(plan.target, 'crash-' + plan.level)
      let targets = [plan.target]
      if (plan.level === 'L2' || plan.level === 'L3') {
        const manager = refs.manager
        const rows = manager && typeof manager.listBundles === 'function' ? await manager.listBundles() : []
        const baselineNames = watchdogState.baseline ? watchdogState.baseline.bundles.map((row) => row.name) : []
        targets = plan.level === 'L3'
          ? rows.filter((row) => row.enabled && !exemptions.has(row.name) && !baselineNames.includes(row.name)).map((row) => row.name)
          : diffBaseline(rows, watchdogState.baseline).changed.map((row) => row.name).concat(plan.target)
        disabled.push(...(await enforcement.disableMany(targets, 'crash-' + plan.level)))
      }
      enforcement.writePendingBoot({
        level: plan.level,
        reason: 'crash',
        attribution: plan.target,
        disable: [...new Set(targets)],
        keep: watchdogState.baseline ? watchdogState.baseline.bundles.map((row) => row.name) : [],
      })
      appendIncident({ kind: 'crash-enforced', level: plan.level, target: plan.target, detail: '已按 ' + plan.level + ' 隔离 ' + disabled.length + ' 个目标（pending-boot.json 供外部包装器下次启动前使用）' })
      persist(true)
    } catch (error) {
      watchdogState.degraded = 'crash enforce: ' + String((error && error.message) || error)
    }
  }

  function readCrashLogs() {
    let entries
    try {
      entries = fs.readdirSync(logsDir, { withFileTypes: true })
    } catch {
      return
    }
    let newest = watchdogState.lastCrashScan
    const crashes = []
    for (const entry of entries) {
      if (!entry.isFile() || !/^crash-.*-host\.log$/.test(entry.name)) continue
      const file = path.join(logsDir, entry.name)
      let stat
      try {
        stat = fs.statSync(file)
      } catch {
        continue
      }
      if (stat.mtimeMs <= watchdogState.lastCrashScan) continue
      const text = readHead(file, CRASH_HEAD_BYTES)
      const parsed = parseCrashLog(text)
      if (!parsed.valid) continue
      crashes.push({ file: entry.name, mtimeMs: stat.mtimeMs, parsed })
      if (stat.mtimeMs > newest) newest = stat.mtimeMs
    }
    if (crashes.length === 0) return
    const records = readJournalRecords(journalDir)
    for (const crash of crashes) {
      watchdogState.escalation.crashes.push(Date.now())
      const recent = watchdogState.escalation.crashes.filter((at) => Date.now() - at <= CRASH_WINDOW_MS)
      watchdogState.escalation.crashes = recent
      const level = escalationLevel(recent.length)
      watchdogState.escalation.level = level
      const attribution = attributeCrash(records, crash.parsed)
      watchdogState.hostRunId = crash.parsed.hostRunId || null
      const plan = {
        at: new Date().toISOString(),
        level,
        remediation: remediationFor('crash'),
        crash: { file: crash.file, time: crash.parsed.time, phase: crash.parsed.phase, oom: crash.parsed.oom, hostExitCode: crash.parsed.hostExitCode },
        hostRunId: crash.parsed.hostRunId,
        target: attribution.picked ? attribution.picked.target : null,
        inFlight: attribution.picked ? Boolean(attribution.picked.inFlight) : false,
        evidence: attribution.candidates,
        reason: attribution.reason,
        applied: false,
      }
      watchdogState.plans.push(plan)
      if (watchdogState.plans.length > 20) watchdogState.plans = watchdogState.plans.slice(-20)
      appendIncident({
        kind: 'crash',
        file: crash.file,
        time: crash.parsed.time,
        phase: crash.parsed.phase,
        hostExitCode: crash.parsed.hostExitCode,
        oom: crash.parsed.oom,
        level,
        target: plan.target,
        inFlight: plan.inFlight,
        detail: plan.target
          ? `崩溃归因：${plan.target}${plan.inFlight ? '（崩溃时操作仍在途）' : ''}；${watchdogState.enforce ? 'enforce' : 'dry-run'}`
          : '崩溃归因失败：本 run 无安装/启用记录，不自动关闭',
      })
      if (watchdogState.enforce && watchdogState.crashFallback === 'enforce') void enforceCrashPlan(plan)
    }
    if (newest > watchdogState.lastCrashScan) {
      watchdogState.lastCrashScan = newest
      persist(true)
    }
  }

  return { readCrashLogs }
}
