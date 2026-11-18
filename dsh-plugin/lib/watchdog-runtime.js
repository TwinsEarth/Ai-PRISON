import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import v8 from 'node:v8'
import crypto from 'node:crypto'
import { watchdogState, MAX_SAMPLES, MAX_INCIDENTS } from './watchdog-state.js'
import { probeBudget } from './budget-probe.js'
import { createTamperMonitor } from './tamper-monitor.js'
import {
  attributeCrash,
  classifyWaterline,
  decideToolGuard,
  diffBaseline,
  escalationLevel,
  formatBytes,
  hotOperationTriggered,
  mergeEstimate,
  operationWeight,
  parseCrashLog,
  thresholdsFor,
  topOperations,
} from './watchdog-policy.js'

const SAMPLE_MS = 5_000
const FLUSH_MS = 30_000
const CRASH_HEAD_BYTES = 256 * 1024
const BASELINE_DELAY_MS = 60_000
const PAUSE_GRACE_MS = 10 * 60_000
const CRASH_WINDOW_MS = 10 * 60_000
const MAX_JOURNAL_RECORDS = 4000
const EXEMPT_TARGETS = new Set([
  'dsh-base',
  'dsh-web-app',
  '@deepseek-ai/dsh-base',
  '@deepseek-ai/dsh-web-app',
  '@twinsearth/ai-prison-dsh-plugin',
])

function resolveProfileDir() {
  const fromEnv = typeof process.env.DSH_PROFILE_DIR === 'string' && process.env.DSH_PROFILE_DIR.trim()
  if (fromEnv) return fromEnv
  const name = typeof process.env.DSH_PROFILE === 'string' && process.env.DSH_PROFILE.trim() ? process.env.DSH_PROFILE.trim() : 'desktop'
  return path.join(os.homedir(), '.dsh', 'profiles', name)
}

function resolveLogsDir() {
  const roaming = process.env.APPDATA || path.join(os.homedir(), 'AppData', 'Roaming')
  return path.join(roaming, '@deepseek-ai', 'dsh-desktop', 'logs')
}

function resolveJournalDir() {
  const roaming = process.env.APPDATA || path.join(os.homedir(), 'AppData', 'Roaming')
  return path.join(roaming, '@deepseek-ai', 'dsh-desktop', 'plugin-operations')
}

function readHead(file, bytes) {
  let fd
  try {
    fd = fs.openSync(file, 'r')
    const buffer = Buffer.alloc(bytes)
    const read = fs.readSync(fd, buffer, 0, bytes, 0)
    return buffer.subarray(0, read).toString('utf8')
  } catch {
    return ''
  } finally {
    if (fd !== undefined) {
      try {
        fs.closeSync(fd)
      } catch {
        // ignore
      }
    }
  }
}

function readJournalRecords(journalDir) {
  const records = []
  for (const name of ['operations.jsonl', 'operations.previous.jsonl']) {
    const file = path.join(journalDir, name)
    let text = ''
    try {
      text = fs.readFileSync(file, 'utf8')
    } catch {
      continue
    }
    for (const line of text.split('\n').slice(-MAX_JOURNAL_RECORDS)) {
      const trimmed = line.trim()
      if (!trimmed) continue
      try {
        records.push(JSON.parse(trimmed))
      } catch {
        // ignore partial line
      }
    }
  }
  return records
}

/** Read-only watchdog runtime. Enforcement never touches plugin files, only the manager API. */
export function createWatchdog(ctx, config = {}, logger = {}) {
  const profileDir = config.profileDir ? path.resolve(String(config.profileDir)) : resolveProfileDir()
  const dataDir = config.dataDir ? path.resolve(String(config.dataDir)) : path.join(profileDir, '.ai-prison-watchdog')
  const logsDir = config.logsDir ? path.resolve(String(config.logsDir)) : resolveLogsDir()
  const journalDir = config.journalDir ? path.resolve(String(config.journalDir)) : resolveJournalDir()
  const stateFile = path.join(dataDir, 'state.json')
  const incidentsFile = path.join(dataDir, 'incidents.jsonl')
  const baselineFile = path.join(dataDir, 'baseline.json')
  const log = {
    info: (...args) => (typeof logger.info === 'function' ? logger.info(...args) : undefined),
    warn: (...args) => (typeof logger.warn === 'function' ? logger.warn(...args) : undefined),
  }
  const refs = { manager: null, tools: null }
  const cleanups = []
  const effect = (fn, label) => {
    const disposer = ctx.effect(fn, label)
    if (typeof disposer === 'function') cleanups.push(disposer)
    return disposer
  }

  try {
    fs.mkdirSync(dataDir, { recursive: true })
  } catch (error) {
    watchdogState.degraded = `cannot create ${dataDir}: ${String((error && error.message) || error)}`
  }

  watchdogState.mode = config.enforce === false ? 'observe' : 'enforce'
  watchdogState.enforce = config.enforce !== false
  watchdogState.startedAt = new Date().toISOString()
  watchdogState.runId = crypto.randomUUID()
  watchdogState.dataDir = dataDir
  watchdogState.config = { sampleMs: SAMPLE_MS, ...config }
  watchdogState.crashFallback = config.crashFallback === 'dry-run' ? 'dry-run' : 'enforce'
  watchdogState.probeStarted = false
  watchdogState.lastCrashScan = 0
  watchdogState.baseline = null
  for (const plan of watchdogState.plans) plan.applied = false

  let lastFlush = 0
  let operationWindowUntil = 0
  const operationWindows = []
  let tamperMigrationDone = false
  let tamper = null
  restoreState()
  readCrashLogs()
  setupTamperMonitor()

  function setupTamperMonitor() {
    if (config.tamperMonitor === false) {
      watchdogState.tamper = { enabled: false, policy: 'off', files: [], lastCheckAt: null, lastDiff: [], restored: [], incidents: 0 }
      return
    }
    try {
      tamper = createTamperMonitor({
        profileDir,
        dataDir,
        policy: config.tamperPolicy === 'alert' ? 'alert' : 'restore',
        graceMs: Number(config.tamperGraceMs) >= 0 ? Number(config.tamperGraceMs) : 15_000,
        intervalMs: Number(config.tamperIntervalMs) > 0 ? Number(config.tamperIntervalMs) : 30_000,
        shouldIgnore: () => Date.now() < operationWindowUntil,
        isWithinOperationWindow: (mtimeMs) => {
          const now = Date.now()
          return operationWindows.some((window) => mtimeMs >= window.startedAt - 5000 && mtimeMs <= (window.settledAt || now) + 5000)
        },
        onTamper: handleTamper,
        log: (message) => log.info('[ai-prison-watchdog] ' + message),
      })
      watchdogState.tamper = tamper.status()
      const interval = Number(config.tamperIntervalMs)
      if (interval !== 0) tamper.start()
    } catch (error) {
      watchdogState.degraded = 'tamper monitor: ' + String((error && error.message) || error)
    }
  }

  function refreshTamperStatus() {
    if (tamper) watchdogState.tamper = tamper.status()
  }

  function handleTamper(result) {
    if (!result || !Array.isArray(result.diffs) || result.diffs.length === 0) return
    const files = result.diffs.map((diff) => diff.file)
    appendIncident({ kind: 'tamper', files, policy: result.policy, restored: result.restored, detail: '配置漂移：' + files.join(', ') })
    let target = null
    let evidence = []
    try {
      const records = readJournalRecords(journalDir).filter((record) => {
        const at = Date.parse((record && record.operation && record.operation.time) || 0)
        return Number.isFinite(at) && Date.now() - at < 6 * 3600 * 1000
      })
      const attribution = attributeCrash(records, { hostRunId: null })
      target = attribution.picked ? attribution.picked.target : null
      evidence = attribution.candidates
    } catch {
      target = null
    }
    const applied = Boolean(watchdogState.enforce && target)
    const plan = { at: new Date().toISOString(), level: 'L1', crash: null, reason: 'tamper', target, inFlight: false, evidence: { files: result.diffs, candidates: evidence }, applied }
    watchdogState.plans.push(plan)
    if (watchdogState.plans.length > 20) watchdogState.plans = watchdogState.plans.slice(-20)
    if (applied) {
      watchdogState.quarantine.set(target, { reason: 'tamper', level: 'L1', at: plan.at, release: 'manual', evidence: '配置漂移：' + files.join(', ') })
      void disableTarget(target, 'tamper')
      writePendingBoot({ level: 'L1', reason: 'tamper', disable: [target], keep: [] })
      appendIncident({ kind: 'tamper-enforced', target, files, detail: '配置漂移后已隔离 ' + target })
    }
    persist(true)
  }

  function persist(force = false) {
    const now = Date.now()
    if (!force && now - lastFlush < FLUSH_MS) return
    lastFlush = now
    const payload = {
      startedAt: watchdogState.startedAt,
      lastCrashScan: watchdogState.lastCrashScan,
      incidents: watchdogState.incidents.slice(-50),
      escalation: watchdogState.escalation,
      baseline: watchdogState.baseline,
      budgets: watchdogState.budgets,
      quarantine: [...watchdogState.quarantine].map(([target, entry]) => ({ target, ...entry })),
      migratedTamperQuarantine: tamperMigrationDone,
    }
    try {
      fs.writeFileSync(stateFile, `${JSON.stringify(payload, null, 2)}\n`)
    } catch {
      // ignore
    }
  }

  function restoreState() {
    try {
      const parsed = JSON.parse(fs.readFileSync(stateFile, 'utf8'))
      watchdogState.lastCrashScan = Number(parsed.lastCrashScan) > 0 ? Number(parsed.lastCrashScan) : 0
      if (Array.isArray(parsed.incidents)) watchdogState.incidents = parsed.incidents.slice(-MAX_INCIDENTS)
      if (parsed.escalation && Array.isArray(parsed.escalation.crashes)) watchdogState.escalation = { crashes: parsed.escalation.crashes.slice(-50), level: parsed.escalation.level || 'L0' }
      if (parsed.baseline && Array.isArray(parsed.baseline.bundles)) watchdogState.baseline = parsed.baseline
      if (parsed.budgets && typeof parsed.budgets === 'object') watchdogState.budgets = parsed.budgets
      tamperMigrationDone = parsed.migratedTamperQuarantine === true
      if (Array.isArray(parsed.quarantine)) {
        let dropped = 0
        for (const entry of parsed.quarantine) {
          if (!tamperMigrationDone && entry && entry.reason === 'tamper') {
            dropped += 1
            continue
          }
          if (entry && typeof entry.target === 'string') watchdogState.quarantine.set(entry.target, { reason: entry.reason || 'crash', level: entry.level || 'L1', at: entry.at || new Date().toISOString(), release: entry.release || 'manual', evidence: entry.evidence || '' })
        }
        if (dropped > 0) {
          watchdogState.incidents.push({ at: new Date().toISOString(), mode: watchdogState.mode, kind: 'migration', detail: '清理 ' + dropped + ' 条 1.4.3/1.4.4 误判的 tamper 隔离（操作窗口归因已修复）' })
        }
        tamperMigrationDone = true
      }
    } catch {
      // first run or unreadable state: start clean
    }
  }

  function appendIncident(incident) {
    const record = { at: new Date().toISOString(), mode: watchdogState.mode, ...incident }
    watchdogState.incidents.push(record)
    if (watchdogState.incidents.length > MAX_INCIDENTS) watchdogState.incidents = watchdogState.incidents.slice(-MAX_INCIDENTS)
    try {
      fs.appendFileSync(incidentsFile, `${JSON.stringify(record)}\n`)
      const stat = fs.statSync(incidentsFile)
      if (stat.size > 1024 * 1024) {
        const lines = fs.readFileSync(incidentsFile, 'utf8').trim().split('\n').slice(-500)
        fs.writeFileSync(incidentsFile, `${lines.join('\n')}\n`)
      }
    } catch {
      // auditing is best-effort
    }
    return record
  }

  // ---- memory waterline ----------------------------------------------------
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
    maybeWriteBaseline()
    refreshTamperStatus()
  }

  // ---- clean baseline (requirement 4, dry-run in P1) ----------------------
  function maybeWriteBaseline() {
    if (watchdogState.baseline && watchdogState.baseline.runId === watchdogState.runId) return
    if (!watchdogState.startedAt) return
    const age = Date.now() - Date.parse(watchdogState.startedAt)
    if (age < BASELINE_DELAY_MS) return
    if (watchdogState.waterline === 'pause' || watchdogState.waterline === 'frost') return
    if (watchdogState.quarantine.size > 0) return
    const recentCrash = watchdogState.incidents.some((incident) => incident.kind === 'crash' && Date.parse(incident.at || 0) >= Date.parse(watchdogState.startedAt))
    if (recentCrash) return
    void collectBaseline()
  }

  async function collectBaseline() {
    const manager = refs.manager
    if (!manager || typeof manager.listBundles !== 'function') return
    try {
      const rows = await manager.listBundles()
      const bundles = rows
        .filter((row) => row.enabled)
        .map((row) => ({ name: row.name, version: String(row.version || ''), enabled: true }))
      watchdogState.baseline = {
        at: new Date().toISOString(),
        runId: watchdogState.runId,
        hostRunId: watchdogState.hostRunId || null,
        bundles,
      }
      try {
        fs.writeFileSync(baselineFile, `${JSON.stringify(watchdogState.baseline, null, 2)}\n`)
      } catch {
        // ignore
      }
      appendIncident({ kind: 'baseline', detail: `记录干净基线：${bundles.length} 个 bundle` })
      persist(true)
    } catch (error) {
      watchdogState.degraded = `baseline: ${String((error && error.message) || error)}`
    }
  }

  // ---- plugin operation accounting / hot-op jail (requirement 2) ----------
  const inflight = new Map()

  function onOperation(operation) {
    try {
      if (!operation || typeof operation !== 'object') return
      const target = typeof operation.target === 'string' && operation.target ? operation.target : null
      if (!target) return
      if ((watchdogState.suppressUntil.get(target) || 0) > Date.now()) return
      const entry = watchdogState.operations.get(target) || { count: 0, estimateMB: 0, lastAt: null, version: operation.version || null, lastApplication: null }
      if (operation.phase === 'started') {
        operationWindowUntil = Date.now() + 4000
        operationWindows.push({ startedAt: Date.now(), settledAt: null })
        if (operationWindows.length > 20) operationWindows.shift()
        inflight.set(operation.id, { target, heapBefore: watchdogState.latest ? watchdogState.latest.heapUsed : 0 })
        entry.lastAt = operation.time || entry.lastAt
        watchdogState.operations.set(target, entry)
        return
      }
      if (operation.phase !== 'settled') return
      operationWindowUntil = Date.now() + 4000
      for (let i = operationWindows.length - 1; i >= 0; i -= 1) {
        if (operationWindows[i].settledAt === null) { operationWindows[i].settledAt = Date.now(); break }
      }
      const weight = operationWeight(operation)
      if (weight > 0 && operation.application !== 'failed' && operation.application !== 'cancelled') {
        entry.count = Math.round((Number(entry.count) + weight) * 10) / 10
      }
      const before = inflight.get(operation.id)
      inflight.delete(operation.id)
      if (before && watchdogState.latest && before.heapBefore > 0) {
        const deltaMB = Math.max(0, Math.round((watchdogState.latest.heapUsed - before.heapBefore) / (1024 * 1024)))
        entry.estimateMB = mergeEstimate(entry.estimateMB, deltaMB)
      }
      entry.lastAt = operation.time || entry.lastAt
      entry.version = operation.version || entry.version
      entry.lastApplication = operation.application || null
      watchdogState.operations.set(target, entry)
      if (operation.reason === 'remove' && operation.application === 'applied') {
        watchdogState.operations.delete(target)
        return
      }
      if (watchdogState.enforce && !EXEMPT_TARGETS.has(target) && !watchdogState.quarantine.has(target) && hotOperationTriggered(entry.count)) {
        watchdogState.quarantine.set(target, {
          reason: 'hot-ops',
          level: 'L2',
          at: new Date().toISOString(),
          release: 'restart',
          evidence: `本次运行内热操作计数 ${entry.count} > 5（安装/启用/热切换）`,
        })
        appendIncident({ kind: 'quarantine', target, level: 'L2', reason: 'hot-ops', detail: `热操作 ${entry.count} 次 > 5，自动关闭` })
        persist(true)
        void disableTarget(target, 'hot-ops')
      }
      persist(true)
    } catch (error) {
      watchdogState.degraded = `operation handler: ${String((error && error.message) || error)}`
    }
  }

  async function disableTarget(target, reason) {
    const manager = refs.manager
    if (!manager || typeof manager.setBundleEnabled !== 'function') {
      watchdogState.degraded = `disable ${target}: pluginManager unavailable`
      return false
    }
    watchdogState.suppressUntil.set(target, Date.now() + 8000)
    for (let attempt = 0; attempt < 2; attempt += 1) {
      try {
        const rows = typeof manager.listBundles === 'function' ? await manager.listBundles() : []
        const row = rows.find((entry) => entry.name === target)
        if (row && row.enabled === false) return true
        await manager.setBundleEnabled(target, false)
        appendIncident({ kind: 'close', target, reason, detail: `已自动关闭 ${target}（${reason}）` })
        return true
      } catch (error) {
        if (attempt === 0) await new Promise((resolve) => setTimeout(resolve, 500))
        else {
          appendIncident({ kind: 'close-failed', target, reason, detail: `自动关闭 ${target} 失败：${String((error && error.message) || error)}` })
          watchdogState.degraded = `disable ${target}: ${String((error && error.message) || error)}`
          return false
        }
      }
    }
    return false
  }

  function releaseTarget(target) {
    const entry = watchdogState.quarantine.get(target)
    if (!entry) return { ok: false, error: 'not quarantined' }
    watchdogState.quarantine.delete(target)
    watchdogState.suppressUntil.set(target, Date.now() + 5000)
    appendIncident({ kind: 'release', target, detail: `用户假释 ${target}（原因 ${entry.reason}）` })
    persist(true)
    return { ok: true }
  }

  function writePendingBoot(payload) {
    try {
      fs.writeFileSync(path.join(dataDir, 'pending-boot.json'), JSON.stringify({ writtenAt: new Date().toISOString(), ...payload }, null, 2) + '\n')
    } catch {
      // wrapper reads this file; best effort
    }
  }

  async function disableMany(targets, reason) {
    const unique = [...new Set(targets)].filter((target) => target && !EXEMPT_TARGETS.has(target) && !watchdogState.quarantine.has(target))
    const disabled = []
    for (const target of unique) {
      watchdogState.quarantine.set(target, { reason, level: 'crash', at: new Date().toISOString(), release: 'manual', evidence: reason })
      if (await disableTarget(target, reason)) disabled.push(target)
      await new Promise((resolve) => setTimeout(resolve, 300))
    }
    return disabled
  }

  async function enforceCrashPlan(plan) {
    if (!plan || !plan.target || watchdogState.crashFallback !== 'enforce') return
    try {
      watchdogState.quarantine.set(plan.target, { reason: 'crash', level: plan.level, at: new Date().toISOString(), release: 'manual', evidence: plan.reason })
      const disabled = [plan.target].filter((target) => !EXEMPT_TARGETS.has(target))
      await disableTarget(plan.target, 'crash-' + plan.level)
      let targets = [plan.target]
      if (plan.level === 'L2' || plan.level === 'L3') {
        const manager = refs.manager
        const rows = manager && typeof manager.listBundles === 'function' ? await manager.listBundles() : []
        const baselineNames = watchdogState.baseline ? watchdogState.baseline.bundles.map((row) => row.name) : []
        targets = plan.level === 'L3'
          ? rows.filter((row) => row.enabled && !EXEMPT_TARGETS.has(row.name) && !baselineNames.includes(row.name)).map((row) => row.name)
          : diffBaseline(rows, watchdogState.baseline).changed.map((row) => row.name).concat(plan.target)
        disabled.push(...(await disableMany(targets, 'crash-' + plan.level)))
      }
      writePendingBoot({
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

  async function reapplyQuarantine() {
    for (const [target, entry] of watchdogState.quarantine) {
      if (entry.release === 'restart') {
        watchdogState.quarantine.delete(target)
        continue
      }
      void disableTarget(target, 'boot-reapply')
    }
  }

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
    if (thresholds && mb >= Math.round(thresholds.singleBlock / (1024 * 1024))) {
      appendIncident({ kind: 'plugin-budget', target: name, level: 'block', detail: '启动实测 ' + mb + ' MB > 500 MB，禁止该插件' })
      if (watchdogState.enforce) {
        watchdogState.quarantine.set(name, { reason: 'budget', level: 'L1', at: new Date().toISOString(), release: 'manual', evidence: mb + ' MB > 500 MB' })
        void disableTarget(name, 'budget-over-500MB')
        writePendingBoot({ level: 'L1', reason: 'budget', disable: [name], keep: [] })
      }
    } else if (thresholds && mb >= Math.round(thresholds.singleWarn / (1024 * 1024))) {
      appendIncident({ kind: 'plugin-budget', target: name, level: 'warn', detail: '启动实测 ' + mb + ' MB > 300 MB，仅提醒' })
    }
    persist(true)
    return { ok: true }
  }

  async function scheduleProbe({ force = false } = {}) {
    if (watchdogState.probeRunning) return watchdogState.probeProgress
    const manager = refs.manager
    if (!manager || typeof manager.listBundles !== 'function') return null
    let rows
    try {
      rows = await manager.listBundles()
    } catch {
      return null
    }
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
      .filter((row) => row.enabled && !EXEMPT_TARGETS.has(row.name))
      .map((row) => ({ name: row.name, version: row.version ? String(row.version) : '', state: budgetState(row.name, row.version ? String(row.version) : '') }))
      .filter((candidate) => candidate.state !== 'ok')
      .sort((a, b) => (recent.has(b.name) ? 1 : 0) - (recent.has(a.name) ? 1 : 0))
      .slice(0, force ? Math.max(probeMax, Number(config.probeMaxForce) > 0 ? Number(config.probeMaxForce) : 15) : probeMax)
    watchdogState.probeProgress = { done: 0, total: candidates.length, current: null, finishedAt: null }
    if (candidates.length === 0) return watchdogState.probeProgress
    watchdogState.probeRunning = true
    for (const candidate of candidates) {
      if (watchdogState.waterline !== 'ok') break
      const outcome = await probeOne(candidate.name, candidate.version)
      watchdogState.probeProgress = { ...watchdogState.probeProgress, done: watchdogState.probeProgress.done + 1, current: candidate.name, lastOk: Boolean(outcome && outcome.ok) }
      if (candidates.length > 1) await new Promise((resolve) => setTimeout(resolve, Number(config.probeGapMs) >= 0 ? Number(config.probeGapMs) : 1500))
    }
    watchdogState.probeProgress = { ...watchdogState.probeProgress, current: null, finishedAt: new Date().toISOString() }
    watchdogState.probeRunning = false
    persist(true)
    return watchdogState.probeProgress
  }

  // ---- crash attribution and dry-run plans (requirements 3 & 4) -----------
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

  // ---- registration --------------------------------------------------------
  try {
    if (typeof ctx.inject === 'function') {
      ctx.inject(['pluginManager'], (child) => {
        refs.manager = child.pluginManager || (typeof child.get === 'function' ? child.get('pluginManager') : null)
        if (refs.manager) {
          void reapplyQuarantine()
          const probeTimer = setTimeout(() => {
            void scheduleProbe()
          }, Number(config.probeDelayMs) > 0 ? Number(config.probeDelayMs) : 30_000)
          if (typeof probeTimer.unref === 'function') probeTimer.unref()
        }
      })
    }
  } catch (error) {
    watchdogState.degraded = `pluginManager inject: ${String((error && error.message) || error)}`
  }

  try {
    if (typeof ctx.inject === 'function' && watchdogState.enforce) {
      ctx.inject(['tools'], (child) => {
        refs.tools = child.tools || null
        try {
          if (!child.tools || typeof child.tools.guard !== 'function') return
          child.effect(
            () =>
              child.tools.guard((execution) => {
                const denial = decideToolGuard(execution, watchdogState)
                return denial ? { reason: denial.reason, info: denial.info } : undefined
              }),
            'ai-prison-watchdog: tool guard',
          )
        } catch (error) {
          watchdogState.degraded = `tool guard: ${String((error && error.message) || error)}`
        }
      })
    }
  } catch (error) {
    watchdogState.degraded = `tools inject: ${String((error && error.message) || error)}`
  }

  try {
    const interval = Number(config.sampleMs) > 0 ? Number(config.sampleMs) : SAMPLE_MS
    if (typeof ctx.effect === 'function') {
      effect(() => {
        sample()
        const timer = setInterval(sample, interval)
        return () => clearInterval(timer)
      }, 'ai-prison-watchdog: sampler')
    } else {
      sample()
      setInterval(sample, interval)
    }
  } catch (error) {
    watchdogState.degraded = `sampler: ${String((error && error.message) || error)}`
  }

  try {
    if (typeof ctx.on === 'function') {
      if (typeof ctx.effect === 'function') {
        effect(() => ctx.on('plugin-manager/operation', onOperation), 'ai-prison-watchdog: operations')
      } else {
        ctx.on('plugin-manager/operation', onOperation)
      }
    }
  } catch (error) {
    watchdogState.degraded = `operations: ${String((error && error.message) || error)}`
  }

  try {
    if (typeof ctx.inject === 'function') {
      ctx.inject(['connection'], (child) => {
        try {
          child.effect(
            () =>
              child.connection.fetch.register({
                path: '/api/ai-prison/watchdog/status',
                methods: ['GET'],
                requestBody: 'buffered',
                fetch: async () => Response.json(watchdogSnapshot(), { headers: { 'cache-control': 'no-store' } }),
              }),
            'ai-prison-watchdog: status api',
          )
          child.effect(
            () =>
              child.connection.fetch.register({
                path: '/api/ai-prison/watchdog/action',
                methods: ['POST'],
                requestBody: 'buffered',
                fetch: async (request) => {
                  let body = {}
                  try {
                    body = await request.json()
                  } catch {
                    body = {}
                  }
                  const action = typeof body.action === 'string' ? body.action : ''
                  const target = typeof body.target === 'string' ? body.target : ''
                  if (action === 'probe') {
                    const progress = await scheduleProbe({ force: true })
                    return Response.json({ ok: true, progress })
                  }
                  if (action === 'confirm-pause') {
                    watchdogState.graceUntil = Date.now() + PAUSE_GRACE_MS
                    appendIncident({ kind: 'pause-confirmed', detail: `用户二次确认继续插件操作（宽限 10 分钟）` })
                    return Response.json({ ok: true })
                  }
                  if (action === 'release') return Response.json(releaseTarget(target))
                  if (action === 'close') {
                    const ok = await disableTarget(target, 'manual-close')
                    return Response.json({ ok })
                  }
                  return Response.json({ ok: false, error: 'unknown action' }, { status: 400 })
                },
              }),
            'ai-prison-watchdog: action api',
          )
        } catch (error) {
          watchdogState.degraded = `status api: ${String((error && error.message) || error)}`
        }
      })
    }
  } catch (error) {
    watchdogState.degraded = `connection: ${String((error && error.message) || error)}`
  }

  log.info(`[ai-prison-watchdog] ${watchdogState.mode} 模式已启动（数据目录 ${dataDir}）`)
  return {
    snapshot: watchdogSnapshot,
    sample,
    readCrashLogs,
    probeNow: (options) => scheduleProbe(options),
    tamperCheck: (options) => {
      if (!tamper) return null
      const result = tamper.checkNow(options)
      refreshTamperStatus()
      return result
    },
    disableTarget,
    releaseTarget,
    dispose() {
      while (cleanups.length > 0) {
        const disposer = cleanups.pop()
        try {
          disposer()
        } catch {
          // ignore dispose races
        }
      }
    },
  }
}

/** Merge live operation estimates with measured module-load budgets (measured wins). */
function mergedTop(state, limit) {
  const operations = topOperations(state.operations, 50)
  const measured = []
  for (const [target, entry] of Object.entries(state.budgets || {})) {
    if (!entry || entry.error || entry.moduleLoadMB === undefined) continue
    const existing = operations.find((row) => row.target === target)
    measured.push({
      target,
      count: existing ? existing.count : 0,
      estimateMB: entry.moduleLoadMB,
      lastAt: entry.measuredAt || (existing ? existing.lastAt : null),
      version: entry.version || (existing ? existing.version : null),
      source: 'measured',
      measuredAt: entry.measuredAt || null,
    })
  }
  const others = operations
    .filter((row) => !measured.some((entry) => entry.target === row.target))
    .map((row) => ({ ...row, source: 'estimate' }))
  return [...measured, ...others].sort((a, b) => b.estimateMB - a.estimateMB || b.count - a.count).slice(0, Math.max(1, limit))
}

/** Serialisable snapshot shared by the tool and the panel route. */
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
    budgets: s.budgets,
    tamper: s.tamper || null,
    incidents: s.incidents.slice(-20),
    generatedAt: new Date().toISOString(),
  }
}
