#!/usr/bin/env node
/**
 * aiprison-supervise — external memory supervisor CLI (1.8.0, hidden feature).
 *
 * Runs as its own process, outside the DSH host, and samples that host's RSS at
 * a fixed interval. It never loads into the client, never writes the profile,
 * and never touches the installation. By default it only observes: it records
 * the curve and a verdict. Terminating the host needs `--confirm-kill`.
 *
 *   node bin/aiprison-supervise.mjs --pid 1234
 *   node bin/aiprison-supervise.mjs --pid 1234 --cap-mb 6144 --confirm-kill
 *   node bin/aiprison-supervise.mjs --pid 1234 --interval 500 --dry-run --once
 */
import fs from 'node:fs'
import path from 'node:path'
import { spawnSync } from 'node:child_process'
import { createSupervisorState, evaluateSample, supervisorThresholds, describeVerdict, classifyExit } from '../lib/supervisor.js'

const args = process.argv.slice(2)
const flag = (name) => args.includes(`--${name}`)
const value = (name, fallback) => {
  const i = args.indexOf(`--${name}`)
  return i >= 0 && args[i + 1] && !args[i + 1].startsWith('--') ? args[i + 1] : fallback
}

const pid = Number(value('pid', 0))
if (!Number.isInteger(pid) || pid <= 0) {
  console.error('usage: aiprison-supervise --pid <host-pid> [--cap-mb N] [--interval MS] [--data-dir DIR] [--confirm-kill] [--once]')
  process.exit(2)
}

const interval = Math.max(200, Number(value('interval', 1000)))
const capMB = Number(value('cap-mb', 4096))
const dataDir = path.resolve(value('data-dir', path.join(process.cwd(), '.ai-prison-supervisor')))
const confirmKill = flag('confirm-kill')
const once = flag('once')

const thresholds = supervisorThresholds(capMB * 1024 * 1024, { intervalMs: interval })
const state = createSupervisorState(thresholds)

fs.mkdirSync(dataDir, { recursive: true })
const curveFile = path.join(dataDir, `supervisor-${pid}-${Date.now()}.jsonl`)
const verdictFile = path.join(dataDir, 'supervisor-verdict.json')

function readSample(pidToRead) {
  // One process query returns both memory and CPU seconds, so the extra
  // evidence costs no additional powershell spawn per tick.
  const r = spawnSync(
    'powershell.exe',
    ['-NoProfile', '-Command', `$p=Get-Process -Id ${pidToRead} -ErrorAction SilentlyContinue; if($p){"$([int64]$p.WorkingSet64) $([double]$p.CPU)"}else{"-1"}`],
    { windowsHide: true, encoding: 'utf8' },
  )
  const parts = String(r.stdout || '').trim().split(/\s+/)
  const rss = Number(parts[0])
  if (!Number.isFinite(rss) || rss <= 0) return null
  const cpu = Number(parts[1])
  return { rssBytes: rss, cpuSeconds: Number.isFinite(cpu) ? cpu : undefined }
}

function requestExit(pidToStop) {
  // taskkill without /F asks for a normal termination; /F is the forced fallback.
  spawnSync('taskkill.exe', ['/PID', String(pidToStop)], { windowsHide: true })
}

function forceKill(pidToStop) {
  spawnSync('taskkill.exe', ['/PID', String(pidToStop), '/F'], { windowsHide: true })
}

function writeVerdict(extra = {}) {
  const payload = { at: new Date().toISOString(), pid, thresholds: thresholds, state: { ...state, thresholds: undefined }, summary: describeVerdict(state), ...extra }
  try {
    fs.writeFileSync(verdictFile, JSON.stringify(payload, null, 2))
  } catch {
    // verdict is diagnostic only
  }
}

let stopping = false
let timer = null

function tick() {
  if (stopping) return
  const sample = readSample(pid)
  if (!sample) {
    stopping = true
    if (timer) clearInterval(timer)
    const gapMs = state.lastAt === null ? null : Date.now() - state.lastAt
    const verdict = state.lastAt === null
      ? 'host-gone-before-sampling'
      : classifyExit(state, { gapMs, samples: state.samples, requestedBySupervisor: confirmKill && state.requestedAt !== null })
    writeVerdict({ verdict, gapMs, cpuPeakRatio: state.peakCpuRatio, curveFile: path.basename(curveFile) })
    console.log(`[supervise] ${verdict} ${describeVerdict(state)} gap=${gapMs ?? 'n/a'}ms cpuPeak=${state.peakCpuRatio}`)
    process.exit(0)
  }
  const decision = evaluateSample(state, sample, Date.now())
  try {
    fs.appendFileSync(curveFile, `${JSON.stringify({ t: new Date().toISOString(), rssMB: decision.rssMB, rate: decision.rate, cpuRatio: state.lastCpuRatio, action: decision.action })}\n`)
  } catch {
    // curve file is diagnostic only
  }
  if (decision.action === 'warn') console.log(`[supervise] warn ${decision.reason}`)
  if (decision.action === 'request-exit') {
    console.log(`[supervise] request-exit ${decision.reason}${confirmKill ? '' : ' (observe-only, pass --confirm-kill to act)'}`)
    writeVerdict({ curveFile: path.basename(curveFile) })
    if (confirmKill) {
      requestExit(pid)
      const fallback = setTimeout(() => {
        const alive = readSample(pid)
        if (alive) {
          console.log('[supervise] graceful request did not take effect, forcing kill')
          forceKill(pid)
        }
      }, thresholds.graceMs)
      if (typeof fallback.unref === 'function') fallback.unref()
    }
  }
  if (decision.action === 'kill') {
    console.log(`[supervise] killing ${pid} ${decision.reason}`)
    forceKill(pid)
    writeVerdict({ verdict: 'supervisor-kill', cpuPeakRatio: state.peakCpuRatio, curveFile: path.basename(curveFile) })
    stopping = true
    if (timer) clearInterval(timer)
    process.exit(0)
  }
  if (once) {
    console.log(`[supervise] ${describeVerdict(state)}`)
    writeVerdict({ curveFile: path.basename(curveFile) })
    process.exit(0)
  }
}

console.log(`[supervise] watching pid ${pid} cap=${thresholds.heapLimitMB}MB warn=${thresholds.warnMB}MB kill=${thresholds.killMB}MB interval=${interval}ms`)
tick()
if (!once) {
  timer = setInterval(tick, interval)
  process.on('SIGINT', () => {
    stopping = true
    if (timer) clearInterval(timer)
    writeVerdict({ curveFile: path.basename(curveFile) })
    process.exit(0)
  })
}
