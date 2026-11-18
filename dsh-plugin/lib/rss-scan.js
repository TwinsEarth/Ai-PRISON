import { spawn as defaultSpawn } from 'node:child_process'
import { RSS_QUERY_SCRIPT, parseProcessList } from './rss.js'

/**
 * Child-process RSS scanner (Windows).
 *
 * Prefers the Host subprocess service (collected stdout + abort signal) and
 * falls back to a direct spawn. Failures are recorded on the watchdog state and
 * reported once per run instead of silently producing an empty Top5.
 */
const INTERVAL_MS = 60_000

export function startRssScannerV2({ ctx, refs, profileDir, watchdogState, appendIncident, exemptTargets, spawn = defaultSpawn }) {
  if (process.platform !== 'win32') return undefined
  if (watchdogState.config && watchdogState.config.rssScan === false) return undefined
  if (refs.rssStarted) return undefined
  refs.rssStarted = true
  let scanning = false
  let failureReported = false
  let timer = null

  function service() {
    try {
      return typeof ctx.get === 'function' ? ctx.get('subprocess') : null
    } catch {
      return null
    }
  }

  function direct() {
    return new Promise((resolve, reject) => {
      const child = spawn('powershell.exe', ['-NoProfile', '-ExecutionPolicy', 'Bypass', '-Command', RSS_QUERY_SCRIPT], { windowsHide: true, stdio: ['ignore', 'pipe', 'pipe'] })
      let stdout = ''
      let stderr = ''
      child.stdout.on('data', (chunk) => {
        stdout += String(chunk)
      })
      child.stderr.on('data', (chunk) => {
        stderr = (stderr + String(chunk)).slice(-500)
      })
      child.on('error', (error) => reject(error))
      child.on('exit', (code) => {
        if (code === 0) resolve({ stdout })
        else reject(new Error('powershell exit ' + code + ': ' + stderr))
      })
    })
  }

  async function viaService(serviceInstance) {
    const controller = new AbortController()
    const abortTimer = setTimeout(() => controller.abort(), 20_000)
    if (typeof abortTimer.unref === 'function') abortTimer.unref()
    try {
      const handle = serviceInstance.spawn({
        argv: ['powershell.exe', '-NoProfile', '-ExecutionPolicy', 'Bypass', '-Command', RSS_QUERY_SCRIPT],
        cwd: profileDir,
        stdio: { stdin: 'ignore', stdout: { maxBytes: 4 * 1024 * 1024 }, stderr: { maxBytes: 64 * 1024 } },
        graceMs: 5000,
        signal: controller.signal,
      })
      await handle.done
      const stdout = handle.collected.stdout ? handle.collected.stdout.readFrom(0).text : ''
      const stderr = handle.collected.stderr ? handle.collected.stderr.readFrom(0).text : ''
      if (!stdout) throw new Error('subprocess service returned no stdout' + (stderr ? ': ' + stderr.slice(0, 300) : ''))
      return { stdout }
    } finally {
      clearTimeout(abortTimer)
    }
  }

  async function scan() {
    if (scanning) return
    const manager = refs.manager
    if (!manager || typeof manager.listBundles !== 'function') return
    scanning = true
    const attemptedAt = new Date().toISOString()
    watchdogState.rssAttempts = (watchdogState.rssAttempts || 0) + 1
    try {
      let result
      const serviceInstance = service()
      if (serviceInstance && typeof serviceInstance.spawn === 'function') {
        try {
          result = await viaService(serviceInstance)
        } catch (error) {
          watchdogState.rssServiceError = String((error && error.message) || error).slice(0, 300)
        }
      }
      if (!result) result = await direct()
      const rows = await manager.listBundles()
      const enabled = rows.filter((row) => row.enabled && !exemptTargets.has(row.name)).map((row) => row.name)
      const parsed = parseProcessList(result.stdout, enabled)
      const rss = { ...(watchdogState.rss || {}) }
      for (const [name, bytes] of Object.entries(parsed.rss)) rss[name] = bytes
      watchdogState.rss = rss
      watchdogState.rssAt = attemptedAt
      watchdogState.rssError = null
      watchdogState.rssStats = { lines: parsed.lines, matched: parsed.matched, names: Object.keys(parsed.rss).length }
      failureReported = false
    } catch (error) {
      const message = String((error && error.message) || error).slice(0, 300)
      watchdogState.rssAt = attemptedAt
      watchdogState.rssError = message
      watchdogState.degraded = 'rss scan: ' + message
      if (!failureReported) {
        failureReported = true
        try {
          appendIncident({ kind: 'rss-scan-failed', detail: '子进程 RSS 扫描失败：' + message })
        } catch {
          // best effort
        }
      }
    } finally {
      scanning = false
    }
  }

  const first = setTimeout(scan, 5000)
  if (typeof first.unref === 'function') first.unref()
  timer = setInterval(scan, INTERVAL_MS)
  if (typeof timer.unref === 'function') timer.unref()
  refs.rssTimer = timer
  return { scan, timer }
}
