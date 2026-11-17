import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import v8 from 'node:v8'
import { watchdogState, MAX_SAMPLES, MAX_INCIDENTS } from './watchdog-state.js'
import {
  classifyWaterline,
  formatBytes,
  mergeEstimate,
  operationWeight,
  parseCrashLog,
  thresholdsFor,
  topOperations,
} from './watchdog-policy.js'

const SAMPLE_MS = 5_000
const FLUSH_MS = 30_000
const CRASH_HEAD_BYTES = 8 * 1024
const LEVEL_RANK = { ok: 0, warn: 1, pause: 2, block: 3 }

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

/** Read-only watchdog runtime. It observes; it never enables or disables plugins. */
export function createWatchdog(ctx, config = {}, logger = {}) {
  const profileDir = resolveProfileDir()
  const dataDir = config.dataDir ? path.resolve(String(config.dataDir)) : path.join(profileDir, '.ai-prison-watchdog')
  const logsDir = config.logsDir ? path.resolve(String(config.logsDir)) : resolveLogsDir()
  const stateFile = path.join(dataDir, 'state.json')
  const incidentsFile = path.join(dataDir, 'incidents.jsonl')
  const log = {
    info: (...args) => (typeof logger.info === 'function' ? logger.info(...args) : undefined),
    warn: (...args) => (typeof logger.warn === 'function' ? logger.warn(...args) : undefined),
  }

  try {
    fs.mkdirSync(dataDir, { recursive: true })
  } catch (error) {
    watchdogState.degraded = `cannot create ${dataDir}: ${String((error && error.message) || error)}`
  }

  watchdogState.mode = 'observe'
  watchdogState.startedAt = new Date().toISOString()
  watchdogState.dataDir = dataDir
  watchdogState.config = { sampleMs: SAMPLE_MS, ...config }
  watchdogState.lastCrashScan = 0
  let lastFlush = 0
  const cleanups = []
  const effect = (fn, label) => {
    const disposer = ctx.effect(fn, label)
    if (typeof disposer === 'function') cleanups.push(disposer)
    return disposer
  }

  restoreState()
  readCrashLogs()

  function restoreState() {
    try {
      const parsed = JSON.parse(fs.readFileSync(stateFile, 'utf8'))
      watchdogState.lastCrashScan = Number(parsed.lastCrashScan) > 0 ? Number(parsed.lastCrashScan) : 0
      if (Array.isArray(parsed.incidents)) {
        watchdogState.incidents = parsed.incidents.slice(-MAX_INCIDENTS)
      }
    } catch {
      // first run or unreadable state: start clean
    }
  }

  function appendIncident(incident) {
    const record = { at: new Date().toISOString(), mode: watchdogState.mode, ...incident }
    watchdogState.incidents.push(record)
    if (watchdogState.incidents.length > MAX_INCIDENTS) {
      watchdogState.incidents = watchdogState.incidents.slice(-MAX_INCIDENTS)
    }
    try {
      fs.appendFileSync(incidentsFile, `${JSON.stringify(record)}\n`)
      const stat = fs.statSync(incidentsFile)
      if (stat.size > 1024 * 1024) {
        const lines = fs.readFileSync(incidentsFile, 'utf8').trim().split('\n').slice(-500)
        fs.writeFileSync(incidentsFile, `${lines.join('\n')}\n`)
      }
    } catch {
      // auditing is best-effort in observe mode
    }
    return record
  }

  function persist(force = false) {
    const now = Date.now()
    if (!force && now - lastFlush < FLUSH_MS) return
    lastFlush = now
    const payload = {
      startedAt: watchdogState.startedAt,
      lastCrashScan: watchdogState.lastCrashScan,
      incidents: watchdogState.incidents.slice(-50),
    }
    try {
      fs.writeFileSync(stateFile, `${JSON.stringify(payload, null, 2)}\n`)
    } catch {
      // ignore
    }
  }

  function sample() {
    let heap
    try {
      heap = v8.getHeapStatistics()
    } catch {
      heap = { heap_size_limit: 0, used_heap_size: 0, total_heap_size: 0 }
    }
    const usage = process.memoryUsage()
    const heapLimit = Number(heap.heap_size_limit) > 0 ? Number(heap.heap_size_limit) : 4 * 1024 * 1024 * 1024
    watchdogState.thresholds = thresholdsFor(heapLimit)
    const heapUsed = Number(usage.heapUsed) > 0 ? Number(usage.heapUsed) : Number(heap.used_heap_size) || 0
    const waterline = classifyWaterline(heapUsed, watchdogState.thresholds)
    const previous = watchdogState.waterline
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
    watchdogState.samples.push(record)
    if (watchdogState.samples.length > MAX_SAMPLES) {
      watchdogState.samples = watchdogState.samples.slice(-MAX_SAMPLES)
    }
    if (waterline !== previous && LEVEL_RANK[waterline] > LEVEL_RANK[previous]) {
      const incident = appendIncident({
        kind: 'waterline',
        level: waterline,
        heapUsed,
        heapLimit,
        detail: `heap ${formatBytes(heapUsed)} / limit ${formatBytes(heapLimit)}`,
      })
      log.warn(`[ai-prison-watchdog] waterline ${waterline}: ${incident.detail}（observe 模式，不关闭插件）`)
      persist(true)
    }
  }

  /** target -> { heapBefore } for operations currently in flight. */
  const inflight = new Map()

  function onOperation(operation) {
    try {
      if (!operation || typeof operation !== 'object') return
      const target = typeof operation.target === 'string' && operation.target ? operation.target : null
      if (!target || operation.reason === 'remove') {
        if (operation.phase === 'settled' && target) {
          watchdogState.operations.delete(target)
        }
        return
      }
      const entry = watchdogState.operations.get(target) || {
        count: 0,
        estimateMB: 0,
        lastAt: null,
        version: operation.version || null,
        lastApplication: null,
      }
      if (operation.phase === 'started') {
        inflight.set(operation.id, { target, heapBefore: watchdogState.latest ? watchdogState.latest.heapUsed : 0 })
        entry.lastAt = operation.time || entry.lastAt
        watchdogState.operations.set(target, entry)
        return
      }
      if (operation.phase === 'settled') {
        const weight = operationWeight(operation)
        if (weight > 0 && operation.application !== 'failed') {
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
        const thresholds = watchdogState.thresholds
        if (thresholds && entry.estimateMB >= Math.round(thresholds.singleWarn / (1024 * 1024))) {
          appendIncident({
            kind: 'plugin-estimate',
            target,
            estimateMB: entry.estimateMB,
            level: entry.estimateMB >= Math.round(thresholds.singleBlock / (1024 * 1024)) ? 'block' : 'warn',
            detail: `${target} 估算 ${entry.estimateMB} MB（observe 模式，仅提醒）`,
          })
        }
        persist(true)
      }
    } catch (error) {
      watchdogState.degraded = `operation handler: ${String((error && error.message) || error)}`
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
      const parsed = parseCrashLog(readHead(file, CRASH_HEAD_BYTES))
      if (!parsed.valid) continue
      appendIncident({
        kind: 'crash',
        file: entry.name,
        time: parsed.time,
        phase: parsed.phase,
        hostExitCode: parsed.hostExitCode,
        oom: parsed.oom,
        detail: parsed.oom ? 'JavaScript heap out of memory' : `host exited with ${parsed.hostExitCode ?? '?'}`,
      })
      if (stat.mtimeMs > newest) newest = stat.mtimeMs
    }
    if (newest > watchdogState.lastCrashScan) {
      watchdogState.lastCrashScan = newest
      persist(true)
    }
  }

  // Sampler (P0: observe only).
  try {
    if (typeof ctx.effect === 'function') {
      effect(() => {
        sample()
        const interval = Number(config.sampleMs) > 0 ? Number(config.sampleMs) : SAMPLE_MS
        const timer = setInterval(sample, interval)
        return () => clearInterval(timer)
      }, 'ai-prison-watchdog: sampler')
    } else {
      sample()
      setInterval(sample, SAMPLE_MS)
    }
  } catch (error) {
    watchdogState.degraded = `sampler: ${String((error && error.message) || error)}`
  }

  // Plugin-operation accounting.
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

  // Read-only HTTP snapshot for the panel.
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
                fetch: async () =>
                  Response.json(watchdogSnapshot(), { headers: { 'cache-control': 'no-store' } }),
              }),
            'ai-prison-watchdog: status api',
          )
        } catch (error) {
          watchdogState.degraded = `status api: ${String((error && error.message) || error)}`
        }
      })
    }
  } catch (error) {
    watchdogState.degraded = `connection: ${String((error && error.message) || error)}`
  }

  log.info(`[ai-prison-watchdog] observe 模式已启动（数据目录 ${dataDir}）`)
  return {
    snapshot: watchdogSnapshot,
    sample,
    readCrashLogs,
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

/** Serialisable snapshot shared by the tool and the panel route. */
export function watchdogSnapshot() {
  const s = watchdogState
  return {
    ok: Boolean(s.startedAt),
    mode: s.mode,
    startedAt: s.startedAt,
    dataDir: s.dataDir,
    degraded: s.degraded,
    heap: s.latest,
    thresholds: s.thresholds,
    waterline: s.waterline,
    samples: s.samples.slice(-30),
    operations: topOperations(s.operations, 10),
    incidents: s.incidents.slice(-20),
    generatedAt: new Date().toISOString(),
  }
}
