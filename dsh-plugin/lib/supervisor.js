/**
 * External memory supervisor — pure decision logic (1.8.0).
 *
 * Covers the two blind spots the in-process watchdog cannot see:
 *   1. growth while the host JS thread is synchronously blocked (no timer ticks), and
 *   2. growth before the plugin is loaded (startup OOM).
 *
 * A separate OS process samples the host RSS from outside and decides when to
 * warn, request a graceful exit, or force a kill. This module holds no timers,
 * opens no files, and never touches the client installation, so it is testable
 * in isolation. Constraints (see docs/AI-PRISON-HARDENING-1.6.0.md §8):
 *   - never replace app.asar / modify the install dir;
 *   - independent process, OS-level RSS reads only;
 *   - writes nothing to the client profile;
 *   - hidden by default: no panel control, no tool, no config;
 *   - observe-first: forced termination requires an explicit flag.
 */
export const SUPERVISOR_DEFAULTS = {
  intervalMs: 1000,
  strikeLimit: 3,
  warnRatio: 0.73,
  killRatio: 0.93,
  rateMBps: 150,
  rateStrikes: 2,
  graceMs: 5000,
  staleSamples: 3,
  cpuSaturation: 0.8,
}

/** Absolute thresholds derived from the host heap cap (same ratios as the in-process waterline). */
export function supervisorThresholds(heapLimitBytes, overrides = {}) {
  const limitBytes = Number(heapLimitBytes)
  const cap = Number.isFinite(limitBytes) && limitBytes > 0 ? limitBytes : 4 * 1024 * 1024 * 1024
  const config = { ...SUPERVISOR_DEFAULTS, ...overrides }
  const capMB = cap / (1024 * 1024)
  return {
    ...config,
    heapLimitMB: Math.round(capMB),
    warnMB: Math.round(capMB * config.warnRatio),
    killMB: Math.round(capMB * config.killRatio),
  }
}

export function createSupervisorState(thresholds) {
  return {
    thresholds,
    strikes: 0,
    rateStrikes: 0,
    samples: 0,
    peakMB: 0,
    lastAt: null,
    lastRssMB: null,
    lastCpuSeconds: null,
    lastCpuRatio: 0,
    peakCpuRatio: 0,
    requestedAt: null,
    verdict: 'ok',
    reason: null,
  }
}

/**
 * Feed one RSS sample. Returns the decision for this tick.
 * action: ok | warn | request-exit | kill
 * `request-exit` is emitted once; a tick after graceMs turns it into `kill`.
 */
export function evaluateSample(state, sample, now = Date.now()) {
  const t = state.thresholds
  const rssMB = Math.max(0, Math.round((Number(sample.rssBytes) || 0) / (1024 * 1024)))
  const gapMs = state.lastAt === null ? 0 : now - state.lastAt
  const rate = state.lastAt !== null && gapMs > 0 ? Math.round(((rssMB - state.lastRssMB) / (gapMs / 1000)) * 10) / 10 : 0

  // CPU seconds come from the OS counter, so they keep advancing while the host's
  // JS thread is blocked. A pegged ratio next to a plateau is the only external
  // evidence that the process is spinning rather than filling its heap.
  const cpuSeconds = Number(sample.cpuSeconds)
  if (Number.isFinite(cpuSeconds) && state.lastCpuSeconds !== null && gapMs > 0) {
    const ratio = (cpuSeconds - state.lastCpuSeconds) / (gapMs / 1000)
    state.lastCpuRatio = Math.max(0, Math.min(8, Math.round(ratio * 100) / 100))
    state.peakCpuRatio = Math.max(state.peakCpuRatio, state.lastCpuRatio)
  }
  if (Number.isFinite(cpuSeconds)) state.lastCpuSeconds = cpuSeconds

  state.samples += 1
  state.peakMB = Math.max(state.peakMB, rssMB)
  state.lastAt = now
  state.lastRssMB = rssMB

  let action = 'ok'
  let reason = null

  if (rssMB >= t.warnMB) {
    state.strikes = rssMB >= t.killMB ? state.strikes + 1 : 0
    action = 'warn'
    reason =
      rssMB >= t.killMB
        ? `rss ${rssMB} MB >= kill line ${t.killMB} MB (${state.strikes}/${t.strikeLimit})`
        : `rss ${rssMB} MB >= warn line ${t.warnMB} MB`
  } else {
    state.strikes = 0
  }

  if (rate >= t.rateMBps) state.rateStrikes += 1
  else if (rate <= 0) state.rateStrikes = 0

  const rising = rate >= t.rateMBps && state.rateStrikes >= t.rateStrikes
  const atKillLine = rssMB >= t.killMB && state.strikes >= t.strikeLimit

  if (atKillLine || rising) {
    action = 'request-exit'
    reason = atKillLine ? reason : `growth ${rate} MB/s >= ${t.rateMBps} MB/s on ${state.rateStrikes} samples`
  }
  if (state.requestedAt !== null && action === 'request-exit') action = 'ok'

  if (state.requestedAt !== null && now - state.requestedAt >= t.graceMs) {
    action = 'kill'
    reason = `still running ${Math.round((now - state.requestedAt) / 1000)}s after the graceful request`
  }
  if (action === 'request-exit') state.requestedAt = now
  if (action === 'kill') state.verdict = 'kill'
  else if (action === 'request-exit') state.verdict = 'request-exit'
  else if (action === 'warn') state.verdict = 'warn'
  state.reason = reason || state.reason
  return { action, rssMB, rate, strikes: state.strikes, reason }
}

/**
 * Post-mortem classification from the recorded curve plus the exit status.
 * The curve is the only evidence left once the host process is gone.
 */
export function classifyExit(state, { exitCode = null, signal = null, gapMs = null, samples = null, killed = false, requestedBySupervisor = false } = {}) {
  const seen = Number.isFinite(samples) ? samples : state.samples
  const peakMB = state.peakMB
  const t = state.thresholds
  if (killed) return 'supervisor-kill'
  const abnormal = exitCode !== 0 || signal !== null
  if (!abnormal) return 'clean-exit'
  if (seen === 0) return 'startup-died-before-sampling'
  // We asked it to quit and it did: the memory reading explains why we asked,
  // not why it died, so do not dress this up as a crash. Observe-only runs
  // never set this, because there we only logged the request.
  if (requestedBySupervisor) return 'exit-requested'
  // Sampling gaps only grow when the sampler itself was starved; a host that
  // simply vanished is caught on the very next tick, so this branch rarely fires.
  if (Number.isFinite(gapMs) && gapMs >= t.intervalMs * t.staleSamples && peakMB >= t.warnMB) return 'sync-blocked'
  // The reachable signature: high memory that never reached the kill line,
  // with the CPU pegged right up to death. A heap that was truly exhausted
  // climbs past the kill line instead.
  const cpuPegged = state.peakCpuRatio >= (t.cpuSaturation ?? 0.8)
  if (peakMB >= t.warnMB && peakMB < t.killMB && cpuPegged) return 'sync-blocked'
  if (peakMB >= t.warnMB) return 'heap-oom'
  if (exitCode === 1) return 'exit-1'
  return 'abnormal-exit'
}

/** One-line human summary for the incident log. */
export function describeVerdict(state) {
  const t = state.thresholds
  const parts = [
    `verdict=${state.verdict}`,
    `peak=${state.peakMB}MB`,
    `samples=${state.samples}`,
    `cap=${t.heapLimitMB}MB`,
    `warn=${t.warnMB}MB`,
    `kill=${t.killMB}MB`,
  ]
  if (state.reason) parts.push(`reason=${state.reason}`)
  return parts.join(' ')
}
