import fs from 'node:fs'
import path from 'node:path'
import crypto from 'node:crypto'
import { watchdogState } from './watchdog-state.js'
import { createBootWatch } from './boot-watch.js'
import { createTamperMonitor, normalizeTamperPolicy } from './tamper-monitor.js'
import { startRssScannerV2 } from './rss-scan.js'
import { decideToolGuard, prunePluginState } from './watchdog-policy.js'
import { BASELINE_DELAY_MS, EXEMPT_TARGETS, SAMPLE_MS, createLogger, resolveJournalDir, resolveLogsDir, resolveProfileDir } from './watchdog-core.js'
import { createPersistence } from './watchdog-persistence.js'
import { createEnforcement } from './watchdog-enforcement.js'
import { createCrash } from './watchdog-crash.js'
import { createBudget } from './watchdog-budget.js'
import { createSampler } from './watchdog-sampler.js'
import { registerWatchdogApi } from './watchdog-api.js'
import { watchdogSnapshot } from './watchdog-snapshot.js'

/**
 * Watchdog orchestrator.
 *
 * Wires the durable state, sampler, operation enforcement, crash plans, budget
 * probes, tamper monitor, startup watch and HTTP surface together. Every module
 * receives only the dependencies it needs; this file keeps the registration and
 * lifecycle logic (Cordis inject/effect) that must stay in one place.
 */
export function createWatchdog(ctx, config = {}, logger = {}) {
  const profileDir = config.profileDir ? path.resolve(String(config.profileDir)) : resolveProfileDir()
  const dataDir = config.dataDir ? path.resolve(String(config.dataDir)) : path.join(profileDir, '.ai-prison-watchdog')
  const logsDir = config.logsDir ? path.resolve(String(config.logsDir)) : resolveLogsDir()
  const journalDir = config.journalDir ? path.resolve(String(config.journalDir)) : resolveJournalDir()
  const stateFile = path.join(dataDir, 'state.json')
  const incidentsFile = path.join(dataDir, 'incidents.jsonl')
  const baselineFile = path.join(dataDir, 'baseline.json')
  const log = createLogger(logger)
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
  watchdogState.boot = null
  watchdogState.heap6g = false
  watchdogState.lastCrashScan = 0
  watchdogState.baseline = null
  for (const plan of watchdogState.plans) plan.applied = false

  // ---- persistence, enforcement, crash, budget ---------------------------
  const persistence = createPersistence({ watchdogState, stateFile, incidentsFile })
  persistence.restoreState()

  const enforcement = createEnforcement({
    watchdogState,
    refs,
    dataDir,
    journalDir,
    exemptions: EXEMPT_TARGETS,
    appendIncident: persistence.appendIncident,
    persist: persistence.persist,
  })

  const crash = createCrash({
    watchdogState,
    logsDir,
    journalDir,
    refs,
    exemptions: EXEMPT_TARGETS,
    appendIncident: persistence.appendIncident,
    persist: persistence.persist,
    enforcement,
  })

  const budget = createBudget({
    watchdogState,
    refs,
    profileDir,
    journalDir,
    exemptions: EXEMPT_TARGETS,
    appendIncident: persistence.appendIncident,
    persist: persistence.persist,
    enforcement,
    config,
  })

  // ---- startup watch -----------------------------------------------------
  let bootWatch = null
  try {
    bootWatch = createBootWatch({
      dataDir,
      watchdogState,
      onTrigger: (trigger) => {
        persistence.appendIncident({ kind: 'startup-memory', detail: '启动期内存失控：' + trigger.reason, rateMBps: trigger.rateMBps, totalMB: trigger.totalMB })
        enforcement.writePendingBoot({
          level: 'L3',
          reason: 'startup-memory',
          safeBoot: true,
          disable: [],
          keep: ['@deepseek-ai/dsh-base', '@deepseek-ai/dsh-web-app', '@twinsearth/ai-prison-dsh-plugin'],
        })
      },
      onFinish: (summary) => {
        watchdogState.boot = summary
      },
    })
    watchdogState.boot = bootWatch.summary()
  } catch (error) {
    watchdogState.degraded = 'boot watch: ' + String((error && error.message) || error)
  }

  // Scan on the next microtask: ctx.inject resolves pluginManager later in create().
  queueMicrotask(() => crash.readCrashLogs())

  // ---- tamper monitor ----------------------------------------------------
  let tamper = null
  function setupTamperMonitor() {
    if (config.tamperMonitor === false) {
      watchdogState.tamper = { enabled: false, policy: 'off', files: [], lastCheckAt: null, lastDiff: [], restored: [], incidents: 0 }
      return
    }
    try {
      tamper = createTamperMonitor({
        profileDir,
        dataDir,
        policy: normalizeTamperPolicy(config.tamperPolicy),
        graceMs: Number(config.tamperGraceMs) >= 0 ? Number(config.tamperGraceMs) : 15_000,
        intervalMs: Number(config.tamperIntervalMs) > 0 ? Number(config.tamperIntervalMs) : 30_000,
        shouldIgnore: () => enforcement.shouldIgnoreWrites(),
        isWithinOperationWindow: (mtimeMs) => enforcement.isWithinOperationWindow(mtimeMs),
        onTamper: (result) => enforcement.handleTamper(result),
        log: (message) => log.info('[ai-prison-watchdog] ' + message),
      })
      watchdogState.tamper = tamper.status()
      const interval = Number(config.tamperIntervalMs)
      if (interval !== 0) tamper.start()
    } catch (error) {
      watchdogState.degraded = 'tamper monitor: ' + String((error && error.message) || error)
    }
  }
  setupTamperMonitor()

  function refreshTamperStatus() {
    if (tamper) watchdogState.tamper = tamper.status()
  }

  // ---- clean baseline ----------------------------------------------------
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
      persistence.appendIncident({ kind: 'baseline', detail: `记录干净基线：${bundles.length} 个 bundle` })
      persistence.persist(true)
    } catch (error) {
      watchdogState.degraded = `baseline: ${String((error && error.message) || error)}`
    }
  }

  // ---- sampler -----------------------------------------------------------
  const sampler = createSampler({
    watchdogState,
    persist: persistence.persist,
    appendIncident: persistence.appendIncident,
    log,
    onAfterSample: () => {
      maybeWriteBaseline()
      refreshTamperStatus()
      void enforcement.enforceMemoryTiers()
    },
  })

  // ---- registration ------------------------------------------------------
  try {
    if (typeof ctx.inject === 'function') {
      ctx.inject(['pluginManager'], (child) => {
        refs.manager = child.pluginManager || (typeof child.get === 'function' ? child.get('pluginManager') : null)
        if (refs.manager) {
          if (bootWatch && typeof refs.manager.listBundles === 'function') {
            void refs.manager.listBundles().then((rows) => {
              bootWatch.setBundleCount(rows.length)
              const removed = prunePluginState(watchdogState, rows)
              if (Object.values(removed).some((targets) => targets.length > 0)) persistence.persist(true)
            }).catch(() => {})
          }
          void enforcement.reapplyQuarantine()
          startRssScannerV2({ ctx, refs, profileDir, watchdogState, appendIncident: persistence.appendIncident, exemptTargets: EXEMPT_TARGETS })
          if (config.probeAuto === true) {
            const probeTimer = setTimeout(() => {
              void budget.scheduleProbe()
            }, Number(config.probeDelayMs) > 0 ? Number(config.probeDelayMs) : 30_000)
            if (typeof probeTimer.unref === 'function') probeTimer.unref()
          }
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
        sampler.sample()
        const timer = setInterval(sampler.sample, interval)
        return () => clearInterval(timer)
      }, 'ai-prison-watchdog: sampler')
    } else {
      sampler.sample()
      setInterval(sampler.sample, interval)
    }
  } catch (error) {
    watchdogState.degraded = `sampler: ${String((error && error.message) || error)}`
  }

  try {
    if (typeof ctx.on === 'function') {
      if (typeof ctx.effect === 'function') {
        effect(() => ctx.on('plugin-manager/operation', enforcement.onOperation), 'ai-prison-watchdog: operations')
      } else {
        ctx.on('plugin-manager/operation', enforcement.onOperation)
      }
    }
  } catch (error) {
    watchdogState.degraded = `operations: ${String((error && error.message) || error)}`
  }

  const apiRegistered = registerWatchdogApi({
    ctx,
    watchdogState,
    getSnapshot: watchdogSnapshot,
    actions: {
      scheduleProbe: budget.scheduleProbe,
      releaseTarget: enforcement.releaseTarget,
      disableTarget: enforcement.disableTarget,
      appendIncident: persistence.appendIncident,
    },
    logger,
  })
  if (!apiRegistered) watchdogState.degraded = 'status api: not registered'

  log.info(`[ai-prison-watchdog] ${watchdogState.mode} 模式已启动（数据目录 ${dataDir}）`)
  return {
    snapshot: watchdogSnapshot,
    sample: sampler.sample,
    readCrashLogs: crash.readCrashLogs,
    probeNow: (options) => budget.scheduleProbe(options),
    checkMemoryTiers: () => enforcement.enforceMemoryTiers(),
    tamperCheck: (options) => {
      if (!tamper) return null
      const result = tamper.checkNow(options)
      refreshTamperStatus()
      return result
    },
    disableTarget: enforcement.disableTarget,
    releaseTarget: enforcement.releaseTarget,
    dispose() {
      if (refs.rssTimer) clearInterval(refs.rssTimer)
      refs.rssTimer = null
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

export { watchdogSnapshot }
