import fs from 'node:fs'
import path from 'node:path'

/**
 * Startup-phase memory watch (1.6.0).
 *
 * Samples the host heap every 500 ms from watchdog activation until the clean
 * baseline is recorded (or `maxMs`). Every sample goes to a run-<pid>.jsonl file
 * so that a later boot (or the external guard) can see the curve even when this
 * process dies. Two stricter startup thresholds raise a safe-boot plan:
 *   - growth rate > rateMBps (default 150 MB/s), or
 *   - total heap > ~49% of the heap cap (2.0 GB on a 4 GB host, 3.0 GB on a 6 GB host).
 */
export const BOOT_SAMPLE_MS = 500
export const BOOT_MAX_MS = 120_000
export const BOOT_RATE_MBPS = 150
export const BOOT_TOTAL_RATIO = 0.49
export const RUN_KEEP = 5

export function bootThresholdsFor(heapLimitBytes) {
  const limitGB = Number(heapLimitBytes) / (1024 * 1024 * 1024)
  const totalMB = Math.max(500, Math.round(limitGB * 1024 * BOOT_TOTAL_RATIO))
  return {
    rateMBps: BOOT_RATE_MBPS,
    totalMB,
  }
}

export function trimRunFiles(dataDir, keep = RUN_KEEP) {
  let entries
  try {
    entries = fs.readdirSync(dataDir).filter((name) => /^run-(\d+)-\d+\.jsonl$/.test(name)).map((name) => ({ name, at: Number(name.match(/^run-(\d+)-\d+/)[1]) }))
  } catch {
    return
  }
  entries.sort((a, b) => a.at - b.at)
  while (entries.length > keep) {
    try {
      fs.unlinkSync(path.join(dataDir, entries.shift().name))
    } catch {
      break
    }
  }
}

export function createBootWatch({ dataDir, processLike = process, watchdogState, onTrigger, onFinish, sampleMs = BOOT_SAMPLE_MS, maxMs = BOOT_MAX_MS }) {
  const startedAt = Date.now()
  const pid = processLike.pid
  const runFile = path.join(dataDir, `run-${startedAt}-${pid}.jsonl`)
  trimRunFiles(dataDir)
  const samples = []
  let previous = null
  let triggered = null
  let timer = null
  let finished = false
  let bundleCount = null

  function write(line) {
    try {
      fs.appendFileSync(runFile, `${JSON.stringify(line)}\n`)
    } catch {
      // best effort: the file is diagnostic only
    }
  }

  function thresholds() {
    const limit = processLike.memoryUsage ? 4 * 1024 * 1024 * 1024 : 4 * 1024 * 1024 * 1024
    return bootThresholdsFor(watchdogState && watchdogState.thresholds ? watchdogState.thresholds.heapLimit : limit)
  }

  function tick() {
    if (finished) return
    const now = Date.now()
    const usage = typeof processLike.memoryUsage === 'function' ? processLike.memoryUsage() : { heapUsed: 0, rss: 0, heapTotal: 0 }
    const heapUsed = Number(usage.heapUsed) || 0
    const record = {
      t: new Date(now).toISOString(),
      elapsedMs: now - startedAt,
      heapUsed,
      heapTotal: Number(usage.heapTotal) || 0,
      rss: Number(usage.rss) || 0,
      bundles: bundleCount,
      phase: watchdogState && watchdogState.startedAt ? 'startup' : 'init',
    }
    samples.push(record)
    write(record)

    if (!triggered) {
      const t = thresholds()
      const rate = previous && now > previous.at ? ((heapUsed - previous.heapUsed) / (now - previous.at)) * 1000 / (1024 * 1024) : 0
      const totalMB = heapUsed / (1024 * 1024)
      const reason = totalMB >= t.totalMB ? `startup heap ${Math.round(totalMB)} MB >= ${t.totalMB} MB` : rate >= t.rateMBps ? `startup growth ${Math.round(rate)} MB/s >= ${t.rateMBps} MB/s` : null
      if (reason) {
        triggered = { at: record.t, reason, rateMBps: Math.round(rate), totalMB: Math.round(totalMB) }
        try {
          if (typeof onTrigger === 'function') onTrigger(triggered)
        } catch {
          // the caller records the incident
        }
      }
    }
    previous = { at: now, heapUsed }

    const clean = watchdogState && watchdogState.baseline
    if (clean || now - startedAt >= maxMs) finish('clean-boot')
  }

  function finish(reason) {
    if (finished) return
    finished = true
    if (timer) clearInterval(timer)
    const peak = samples.reduce((max, sample) => Math.max(max, sample.heapUsed), 0)
    const summary = {
      reason,
      runFile: path.basename(runFile),
      samples: samples.length,
      peakMB: Math.round(peak / (1024 * 1024)),
      startedAt: new Date(startedAt).toISOString(),
      durationMs: Date.now() - startedAt,
      triggered,
      thresholds: thresholds(),
    }
    write({ t: new Date().toISOString(), summary })
    try {
      if (typeof onFinish === 'function') onFinish(summary)
    } catch {
      // the caller only mirrors the summary
    }
    return summary
  }

  timer = setInterval(tick, sampleMs)
  if (typeof timer.unref === 'function') timer.unref()
  tick()

  return {
    runFile,
    setBundleCount(count) {
      bundleCount = Number(count) || 0
    },
    tick,
    stop: finish,
    summary() {
      return { runFile: path.basename(runFile), samples: samples.length, triggered }
    },
  }
}
