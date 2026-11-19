import fs from 'node:fs'
import path from 'node:path'
import { attributeCrash, diffBaseline, escalationLevel, parseCrashLog } from './watchdog-policy.js'
import { remediationFor } from './remediation.js'
import { CRASH_HEAD_BYTES, CRASH_RESET_MS, CRASH_WINDOW_MS, readHead, readJournalRecords } from './watchdog-core.js'

const LEVEL_RANK = { L0: 0, L1: 1, L2: 2, L3: 3 }

/** Ordered degradation steps for one crash-plan level. */
export function crashPlanSteps(level) {
  if (level === 'L3') return ['isolate-target', 'revert-changed', 'safe-mode']
  if (level === 'L2') return ['isolate-target', 'revert-changed']
  if (level === 'L1') return ['isolate-target']
  return []
}

/**
 * Crash-log ingestion and graded crash plans.
 *
 * Every new `crash-*-host.log` is parsed, attributed to the last changed plugin
 * of that host run and turned into an L1-L3 degradation plan:
 *   L1: isolate the attributed (or in-flight) plugin only.
 *   L2: L1 + disable plugins changed since the clean baseline.
 *   L3: L2 + safe mode: keep the clean baseline, close every newer plugin.
 * Enforce mode applies the plan through the enforcement module
 * (close/quarantine + pending-boot); observe mode records it.
 * Crash counters reset after CRASH_RESET_MS of stability.
 */
export function createCrash({ watchdogState, logsDir, journalDir, refs, exemptions, appendIncident, persist, enforcement }) {
  function baselineNames() {
    return watchdogState.baseline ? watchdogState.baseline.bundles.map((row) => row.name) : []
  }

  async function rowsNow() {
    const manager = refs.manager
    if (!manager || typeof manager.listBundles !== 'function') return []
    try {
      const rows = await manager.listBundles()
      return Array.isArray(rows) ? rows : []
    } catch {
      return []
    }
  }

  /** Which plugin set a plan should close, for this level and attribution. */
  async function planTargets(level, target) {
    const rows = await rowsNow()
    const keep = baselineNames()
    const candidates = []
    if (target) candidates.push(target)
    if (level === 'L2') {
      if (!watchdogState.baseline) return target ? [target].filter((name) => !exemptions.has(name)) : []
      for (const row of diffBaseline(rows, watchdogState.baseline).changed) {
        if (!exemptions.has(row.name)) candidates.push(row.name)
      }
    }
    if (level === 'L3') {
      if (keep.length === 0) {
        // No clean baseline to protect: close every non-official third-party
        // plugin (never guess a safe set that includes unknown plugins).
        for (const row of rows) {
          if (!row.enabled || exemptions.has(row.name)) continue
          if (row.official === true || String(row.name).startsWith('@deepseek-ai/')) continue
          candidates.push(row.name)
        }
        return [...new Set(candidates)]
      }
      for (const row of rows) {
        if (!row.enabled || exemptions.has(row.name) || keep.includes(row.name)) continue
        candidates.push(row.name)
      }
    }
    return [...new Set(candidates)].filter((name) => name && !exemptions.has(name))
  }

  async function enforceWhenReady(plan) {
    for (let attempt = 0; attempt < 20; attempt += 1) {
      if (refs.manager && typeof refs.manager.listBundles === 'function') {
        await enforceCrashPlan(plan)
        return
      }
      await new Promise((resolve) => setTimeout(resolve, 25))
    }
    watchdogState.degraded = 'crash enforcement deferred: pluginManager unavailable'
  }

  async function enforceCrashPlan(plan) {
    if (!watchdogState.enforce || watchdogState.crashFallback !== 'enforce') return
    try {
      const targets = await planTargets(plan.level, plan.target)
      const keep = baselineNames()
      plan.targets = targets
      plan.keep = keep
      plan.applied = false
      if (plan.target && !exemptions.has(plan.target)) {
        watchdogState.quarantine.set(plan.target, {
          reason: 'crash',
          level: plan.level,
          at: new Date().toISOString(),
          release: 'manual',
          evidence: plan.reason,
          remediation: remediationFor('crash'),
        })
      }
      let disabled = []
      if (plan.target && !exemptions.has(plan.target)) {
        await enforcement.disableTarget(plan.target, 'crash-' + plan.level)
        disabled.push(plan.target)
      }
      const rest = targets.filter((target) => target !== plan.target)
      if (rest.length > 0) disabled.push(...(await enforcement.disableMany(rest, 'crash-' + plan.level)))
      plan.applied = disabled.length > 0
      if (targets.length > 0) {
        enforcement.writePendingBoot({
          level: plan.level,
          reason: 'crash',
          attribution: plan.target || null,
          disable: [...new Set(targets)],
          keep,
        })
      }
      plan.executedAt = new Date().toISOString()
      appendIncident({
        kind: 'crash-enforced',
        level: plan.level,
        target: plan.target || null,
        detail: targets.length > 0
          ? '已按 ' + plan.level + ' 隔离 ' + disabled.length + ' 个目标（pending-boot.json 供外部包装器下次启动前使用）'
          : '降级计划 ' + plan.level + '：无归因且无可隔离目标，仅记录',
      })
      persist(true)
    } catch (error) {
      watchdogState.degraded = 'crash enforce: ' + String((error && error.message) || error)
    }
  }

  /** Counters older than the reset window no longer escalate. */
  function resetIfStable(now) {
    const esc = watchdogState.escalation
    if (!esc || !esc.lastCrashAt) return
    if (now - esc.lastCrashAt > CRASH_RESET_MS) {
      esc.crashes = []
      esc.level = 'L0'
      appendIncident({ kind: 'crash-reset', detail: '连续 ' + Math.round(CRASH_RESET_MS / 60000) + ' 分钟无崩溃，崩溃降级等级已重置为 L0' })
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
      const now = Date.now()
      resetIfStable(now)
      watchdogState.escalation.crashes.push(now)
      watchdogState.escalation.crashes = watchdogState.escalation.crashes.filter((at) => now - at <= CRASH_WINDOW_MS)
      watchdogState.escalation.lastCrashAt = now
      const previousLevel = watchdogState.escalation.level || 'L0'
      watchdogState.escalation.previousLevel = previousLevel
      const level = escalationLevel(watchdogState.escalation.crashes.length)
      watchdogState.escalation.level = level
      const attribution = attributeCrash(records, crash.parsed)
      watchdogState.hostRunId = crash.parsed.hostRunId || null
      const plan = {
        at: new Date().toISOString(),
        level,
        steps: crashPlanSteps(level),
        remediation: remediationFor('crash'),
        crash: { file: crash.file, time: crash.parsed.time, phase: crash.parsed.phase, oom: crash.parsed.oom, hostExitCode: crash.parsed.hostExitCode },
        hostRunId: crash.parsed.hostRunId,
        target: attribution.picked ? attribution.picked.target : null,
        inFlight: attribution.picked ? Boolean(attribution.picked.inFlight) : false,
        evidence: attribution.candidates,
        reason: attribution.reason,
        fallback: attribution.picked ? null : level === 'L1' ? 'observe-only' : level === 'L2' ? 'changed-set' : 'safe-mode',
        targets: [],
        keep: [],
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
          ? '崩溃归因：' + plan.target + (plan.inFlight ? '（崩溃时操作仍在途）' : '') + '；' + (watchdogState.enforce ? 'enforce' : 'dry-run')
          : '崩溃归因失败：本 run 无安装/启用记录' + (level === 'L1' ? '，本档仅记录' : '，降级改为' + (level === 'L2' ? '变更集隔离' : '安全模式')),
      })
      if ((LEVEL_RANK[level] || 0) > (LEVEL_RANK[previousLevel] || 0)) {
        appendIncident({
          kind: 'crash-escalation',
          level,
          notify: true,
          detail: '崩溃降级升级：' + previousLevel + ' → ' + level + '（' + watchdogState.escalation.crashes.length + ' 次崩溃 / ' + Math.round(CRASH_WINDOW_MS / 60000) + ' 分钟窗口）',
        })
      }
      if (watchdogState.enforce && watchdogState.crashFallback === 'enforce') void enforceWhenReady(plan)
    }
    if (newest > watchdogState.lastCrashScan) {
      watchdogState.lastCrashScan = newest
      persist(true)
    }
  }

  return { readCrashLogs, crashPlanSteps }
}
