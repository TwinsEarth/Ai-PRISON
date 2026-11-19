import fs from 'node:fs'
import path from 'node:path'
import { topOperations, rankTopPlugins } from './watchdog-policy.js'
import { watchdogState } from './watchdog-state.js'

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
  return {
    ok: Boolean(s.startedAt),
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
    topFive: mergedTop(s, 5),
    probeProgress: s.probeProgress || null,
    quarantine,
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
