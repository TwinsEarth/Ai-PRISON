import fs from 'node:fs'
import path from 'node:path'
import { topOperations, rankTopPlugins } from './watchdog-policy.js'
import { watchdogState } from './watchdog-state.js'
import { CRASH_RESET_MS, CRASH_WINDOW_MS } from './watchdog-core.js'

/** Top5 v2: measured + estimate + child RSS with EWMA smoothing and rank hysteresis. */
function mergedTop(state, limit) {
  const operations = topOperations(state.operations, 50)
  const { rows, nextRanking } = rankTopPlugins({
    operations,
    budgets: state.budgets || {},
    rss: state.rss || {},
    previous: state.ranking || {},
    limit,
  })
  state.ranking = nextRanking
  return rows.map((row) => ({
    target: row.target,
    count: row.count || 0,
    estimateMB: row.moduleLoadMB || row.operationMB || 0,
    rssMB: row.rssMB || 0,
    totalMB: row.score,
    lastAt: row.measuredAt || null,
    version: row.version || null,
    source: row.source,
  }))
}

/** The pending-boot plan exists, but no external consumer ships with the plugin. */
function pendingBootInfo(state) {
  const file = state.dataDir ? path.join(state.dataDir, 'pending-boot.json') : null
  if (!file || !fs.existsSync(file)) return { exists: false, file: null, consumer: 'none', appliesNextBoot: false }
  let plan = null
  try {
    plan = JSON.parse(fs.readFileSync(file, 'utf8'))
  } catch {
    plan = null
  }
  return {
    exists: true,
    file,
    level: plan && plan.level ? plan.level : null,
    writtenAt: plan && plan.writtenAt ? plan.writtenAt : null,
    consumer: 'none',
    appliesNextBoot: false,
    hint: 'pending-boot 已写入，但当前插件不附带外部消费器：仅本次运行的进程内隔离生效，下次启动需人工处理。',
  }
}

/** Serialisable snapshot shared by the status tool, the panel route and tests. */
export function watchdogSnapshot() {
  const s = watchdogState
  const quarantine = []
  for (const [target, entry] of s.quarantine instanceof Map ? s.quarantine : []) {
    quarantine.push({ target, ...entry })
  }
  const esc = s.escalation || {}
  const now = Date.now()
  const recentCrashes = Array.isArray(esc.crashes) ? esc.crashes.filter((at) => now - at <= CRASH_WINDOW_MS) : []
  const crashLevel = esc.level || (recentCrashes.length >= 3 ? 'L3' : recentCrashes.length >= 2 ? 'L2' : recentCrashes.length >= 1 ? 'L1' : 'L0')
  const nextLevel = crashLevel === 'L0' ? 'L1' : crashLevel === 'L1' ? 'L2' : crashLevel === 'L2' ? 'L3' : null
  const lastPlan = s.plans && s.plans.length > 0 ? s.plans[s.plans.length - 1] : null
  const crashPlan = {
    level: crashLevel,
    previousLevel: esc.previousLevel || null,
    crashes: recentCrashes.length,
    windowMs: CRASH_WINDOW_MS,
    resetMs: CRASH_RESET_MS,
    lastCrashAt: esc.lastCrashAt || null,
    resetAt: esc.lastCrashAt ? esc.lastCrashAt + CRASH_RESET_MS : null,
    nextLevel,
    steps: crashLevel === 'L3' ? ['isolate-target', 'revert-changed', 'safe-mode'] : crashLevel === 'L2' ? ['isolate-target', 'revert-changed'] : crashLevel === 'L1' ? ['isolate-target'] : [],
    lastPlan: lastPlan
      ? { at: lastPlan.at, level: lastPlan.level, target: lastPlan.target || null, inFlight: Boolean(lastPlan.inFlight), applied: Boolean(lastPlan.applied), fallback: lastPlan.fallback || null, targets: lastPlan.targets || [] }
      : null,
  }
  return {
    ok: Boolean(s.startedAt),
    crashPlan,
    mode: s.mode,
    enforce: s.enforce,
    startedAt: s.startedAt,
    runId: s.runId,
    dataDir: s.dataDir,
    degraded: s.degraded,
    heap: s.latest,
    thresholds: s.thresholds,
    waterline: s.waterline,
    strongWarn: s.strongWarn,
    pausedAt: s.pausedAt,
    graceUntil: s.graceUntil,
    frozenAt: s.frozenAt,
    samples: s.samples.slice(-30),
    operations: mergedTop(s, 10),
    topFive: mergedTop(s, 5).map((row) => {
      const tier = s.memoryTiers instanceof Map ? s.memoryTiers.get(row.target) : undefined
      return { ...row, memoryTier: tier ? tier.tier : 'ok', memoryMB: tier ? tier.mb : null }
    }),
    probeProgress: s.probeProgress || null,
    quarantine,
    memoryTiers: s.memoryTiers instanceof Map ? [...s.memoryTiers].map(([target, entry]) => ({ target, ...entry })) : [],
    plans: s.plans.slice(-5),
    escalation: { level: s.escalation.level, crashes: s.escalation.crashes.slice(-10) },
    crashFallback: s.crashFallback || 'enforce',
    baseline: s.baseline ? { at: s.baseline.at, runId: s.baseline.runId, bundles: s.baseline.bundles } : null,
    pendingBoot: pendingBootInfo(s),
    budgets: s.budgets,
    tamper: s.tamper || null,
    boot: s.boot || null,
    heap6g: s.heap6g === true,
    heap6gRequest: s.dataDir && fs.existsSync(path.join(s.dataDir, 'heap6g-request.json')) ? path.join(s.dataDir, 'heap6g-request.json') : null,
    rss: s.rss || {},
    rssAt: s.rssAt || null,
    rssError: s.rssError || null,
    rssStats: s.rssStats || null,
    rssServiceError: s.rssServiceError || null,
    incidents: s.incidents.slice(-20),
    generatedAt: new Date().toISOString(),
  }
}
