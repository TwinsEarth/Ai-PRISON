/**
 * Pure policy math for the AI Prison watchdog.
 *
 * Threshold layout (derived from the real heap limit, absolute values clamped):
 *   totalWarn   73% of the heap cap  -> yellow waterline
 *   totalPause  88% of the cap       -> additive plugin operations need a second confirmation
 *   totalBlock  93% of the cap       -> freeze every plugin operation except the release whitelist
 *   opWarn      ~67% of the cap      -> strong warning once the user continued through pause
 *   opBlock     ~83% of the cap      -> agent-side plugin_manager guard refuses
 * On a 6 GB host that yields 3.0/3.9/5.0 GB and 4.0/5.0 GB operation gates; on a
 * 4 GB host the same ratios map down to 2.92/3.52/3.72 GB and 2.73/3.41 GB.
 */
export const MB = 1024 * 1024
export const GB = 1024 * MB

export const FREEZE_MARGIN = 0.3 * GB
export const HOT_OP_LIMIT = 5

/** Plugin-prison memory tiers (single plugin occupancy, MB). */
export const MEMORY_TIER_MB = Object.freeze({ warn: 300, throttle: 500, close: 1000, uninstall: 2000, allThirdParty: 3000 })

export function memoryTierFor(mb) {
  const value = Number(mb) || 0
  if (value >= MEMORY_TIER_MB.allThirdParty) return 'all-third-party'
  if (value >= MEMORY_TIER_MB.uninstall) return 'uninstall'
  if (value >= MEMORY_TIER_MB.close) return 'close'
  if (value >= MEMORY_TIER_MB.throttle) return 'throttle'
  if (value >= MEMORY_TIER_MB.warn) return 'warn'
  return 'ok'
}
export const ADDITIVE_ACTIONS = new Set(['set_plugin', 'set_bundle', 'install_bundle', 'remove_bundle', 'set_version_exemption'])
export const READONLY_ACTIONS = new Set(['list_plugins', 'list_bundles', 'list_version_exemptions'])

/** Thresholds derived from the host heap limit (absolute values clamped to the cap). */
export function thresholdsFor(heapLimitBytes) {
  const limit = Number(heapLimitBytes) > 0 ? Number(heapLimitBytes) : 4 * GB
  const clamp = (absolute, ratio) => Math.min(absolute, ratio * limit)
  const nominal = {
    heapLimit: limit,
    singleWarn: clamp(300 * MB, 0.075),
    singleBlock: clamp(500 * MB, 0.125),
    totalWarn: clamp(3 * GB, 0.73),
    totalPause: clamp(3.9 * GB, 0.88),
    totalBlock: clamp(5 * GB, 0.93),
    opWarn: clamp(4 * GB, 2 / 3),
    opBlock: clamp(5 * GB, 5 / 6),
  }
  const shift = (value) => Math.max(0, value - FREEZE_MARGIN)
  const effective = {
    totalWarn: shift(nominal.totalWarn),
    totalPause: shift(nominal.totalPause),
    totalBlock: shift(nominal.totalBlock),
    opWarn: shift(nominal.opWarn),
    opBlock: shift(nominal.opBlock),
  }
  // The classifier applies FREEZE_MARGIN exactly once; the top-level values stay
  // nominal for backwards compatibility, while the UI reads only `display`.
  return { ...nominal, nominal, effective, display: effective }
}

/** Waterline for the whole host, with the active level to keep hysteresis. */
export function classifyWaterline(heapUsed, thresholds, previous = 'ok') {
  if (!thresholds || !(heapUsed >= 0)) return 'ok'
  const above = (value, guard) => heapUsed + guard >= value
  if (previous === 'frost' && heapUsed + FREEZE_MARGIN >= thresholds.totalBlock) return 'frost'
  if (heapUsed + FREEZE_MARGIN >= thresholds.totalBlock) return 'frost'
  if (previous === 'pause' && heapUsed + FREEZE_MARGIN >= thresholds.totalPause) return 'pause'
  if (heapUsed + FREEZE_MARGIN >= thresholds.totalPause) return 'pause'
  if (heapUsed + FREEZE_MARGIN >= thresholds.totalWarn) return 'warn'
  return 'ok'
}

/** True when additive plugin operations must be denied at this waterline. */
export function operationsDenied(level) {
  return level === 'pause' || level === 'frost'
}

/** How much one plugin operation counts toward the per-target hot-operation limit. */
export function operationWeight(operation) {
  if (!operation || typeof operation !== 'object') return 0
  const { stage, enabled } = operation
  if (stage === 'install' || stage === 'remove') return 1
  if (stage === 'enable') return enabled === false ? 0.5 : 1
  return 0
}

/** Exponentially weighted estimate of a plugin's heap cost. */
export function mergeEstimate(previous, next, alpha = 0.5) {
  const value = Number(next)
  if (!Number.isFinite(value) || value < 0) return Number(previous) || 0
  const before = Number(previous)
  if (!Number.isFinite(before) || before <= 0) return value
  return Math.round(before * (1 - alpha) + value * alpha)
}

/**
 * Tool guard decision for the agent-facing plugin_manager tool.
 * @returns {{ reason: string, info?: { name: string, code: string } } | undefined}
 */
export function decideToolGuard(execution, state) {
  if (!execution || execution.name !== 'plugin_manager') return undefined
  const args = execution.arguments && typeof execution.arguments === 'object' ? execution.arguments : {}
  const action = typeof args.action === 'string' ? args.action : ''
  if (!action) return undefined
  const target = typeof args.target === 'string' ? args.target : null
  const quarantine = target && state.quarantine instanceof Map ? state.quarantine.get(target) : null
  if (quarantine && action !== 'list_plugins' && action !== 'list_bundles') {
    return {
      reason: `插件 ${target} 在 AI 监狱（${quarantine.reason}，级别 ${quarantine.level}）。需要用户在本机面板点击"假释"后才能操作。`,
      info: { name: 'ai-prison-quarantine', code: 'quarantined' },
    }
  }
  if (READONLY_ACTIONS.has(action)) return undefined
  const memoryTier = state.memoryTiers instanceof Map ? state.memoryTiers.get(target) : undefined
  if (memoryTier && ['throttle', 'close', 'uninstall', 'all-third-party'].includes(memoryTier.tier)) {
    return {
      reason: "插件 " + target + " 内存档位 " + memoryTier.tier + "（" + memoryTier.mb + " MB）已限制增内存操作（增长目标 ≤1MB/s）；请先在面板“插件监狱”处理。",
      info: { name: 'ai-prison-memory-tier', code: 'plugin-memory-throttled' },
    }
  }
  if (!ADDITIVE_ACTIONS.has(action)) return undefined
  const level = state.waterline
  if (level === 'frost') {
    return {
      reason: `堆水位 ${level}（≥${formatBytes(state.thresholds ? state.thresholds.totalBlock : 0)}）已冻结所有插件操作。请先在面板"内存看护"里关闭 Top5 插件释放内存，或重启 DSH。`,
      info: { name: 'ai-prison-freeze', code: 'memory-frozen' },
    }
  }
  if (level === 'pause') {
    const grace = Number.isFinite(state.graceUntil) ? state.graceUntil : 0
    if (!(Date.now() < grace)) {
      return {
        reason: `堆水位 ${level}（≥${formatBytes(state.thresholds ? state.thresholds.totalPause : 0)}）已暂停插件操作，需要用户二次确认。请在面板"内存看护"里确认继续，或重启 DSH。`,
        info: { name: 'ai-prison-pause', code: 'memory-paused' },
      }
    }
  }
  return undefined
}

/** A panel/UI-initiated operation may run only for the release whitelist while frozen. */
export function allowedDuringFreeze(action) {
  return action === 'list_plugins' || action === 'list_bundles' || action === 'list_version_exemptions'
}

/** Should this crash attribution candidate be quarantined? */
export function hotOperationTriggered(count) {
  return Number(count) > HOT_OP_LIMIT
}

/** Parse the header block of a DSH desktop host crash log. */
export function parseCrashLog(text) {
  const source = String(text || '')
  const header = source.slice(0, 6000)
  const time = /^time:\s*(.+)$/m.exec(header)
  const phase = /^phase:\s*(.+)$/m.exec(header)
  const exit = /exited with (\d+)/.exec(source)
  const oom = /heap out of memory|Allocation failed|MarkCompactCollector|Scavenger:/i.test(source)
  const runId = /hostRunId:\s*'([^']+)'/.exec(source)
  const fatal = /FATAL ERROR/.test(source)
  return {
    valid: Boolean(time || exit || fatal),
    time: time ? time[1].trim() : null,
    phase: phase ? phase[1].trim() : null,
    hostExitCode: exit ? Number(exit[1]) : null,
    oom,
    hostRunId: runId ? runId[1] : null,
  }
}

/**
 * Attribute a crash to the last changed plugin.
 *
 * Preference order (per requirement 3, "only close the last installed plugin"):
 *   1. an installation/enable that was still in flight when the host died;
 *   2. the latest settled install/update in the same host run;
 *   3. the latest settled enable in the same host run.
 * Disable/remove records are never candidates. A run id mismatch or an empty
 * candidate list means "unknown": the caller records it and does not kill.
 */
export function attributeCrash(journalRecords, crash) {
  const runId = crash && crash.hostRunId
  const records = Array.isArray(journalRecords) ? journalRecords : []
  const inRun = records.filter((record) => !runId || record.runId === runId)
  const positive = (operation) =>
    operation
    && (operation.stage === 'install' || (operation.stage === 'enable' && operation.enabled !== false))
    && operation.target
  const started = new Map()
  for (const record of inRun) {
    const operation = record.operation
    if (!positive(operation)) continue
    if (operation.phase === 'started') started.set(operation.id, operation)
    if (operation.phase === 'settled') started.delete(operation.id)
  }
  const inFlight = [...started.values()].sort((a, b) => String(b.time).localeCompare(String(a.time)))[0]
  const settled = inRun
    .map((record) => record.operation)
    .filter((operation) => positive(operation) && operation.phase === 'settled')
    .sort((a, b) => String(b.time).localeCompare(String(a.time)))
  const latestInstall = settled.find((operation) => operation.stage === 'install')
  const latestEnable = settled.find((operation) => operation.stage === 'enable')
  const candidates = []
  if (inFlight) candidates.push({ target: inFlight.target, inFlight: true, reason: inFlight.reason, time: inFlight.time, version: inFlight.version || null })
  for (const operation of [latestInstall, latestEnable]) {
    if (operation && !candidates.some((candidate) => candidate.target === operation.target)) {
      candidates.push({ target: operation.target, inFlight: false, reason: operation.reason, time: operation.time, version: operation.version || null })
    }
  }
  return {
    hostRunId: runId || null,
    candidates,
    picked: candidates.length > 0 ? candidates[0] : null,
    reason: candidates.length === 0 ? 'no candidate in this host run (missing or incomplete operation history)' : 'last changed plugin in the crashed host run',
  }
}

/** Difference between current bundle rows and the recorded clean baseline. */
export function diffBaseline(rows, baseline) {
  const baselineBundles = baseline && Array.isArray(baseline.bundles) ? baseline.bundles : []
  const known = new Map(baselineBundles.map((row) => [row.name, String(row.version || '')]))
  const changed = []
  for (const row of Array.isArray(rows) ? rows : []) {
    const version = String(row.version || '')
    if (!row.enabled) continue
    const before = known.get(row.name)
    if (before === undefined) changed.push({ name: row.name, version, kind: 'added' })
    else if (before !== version) changed.push({ name: row.name, version, before, kind: 'updated' })
  }
  const level = escalationLevel(0)
  return { changed, unknown: !baseline, level }
}

/** Escalation level from recent crash count (dry-run plans in P1). */
export function escalationLevel(crashCount) {
  if (crashCount >= 3) return 'L3'
  if (crashCount >= 2) return 'L2'
  if (crashCount >= 1) return 'L1'
  return 'L0'
}

/** Top-N view of per-target accounting, biggest estimate first. */
export function topOperations(operations, limit = 5) {
  const rows = []
  for (const [target, entry] of operations instanceof Map ? operations : Object.entries(operations || {})) {
    rows.push({
      target,
      count: Number(entry.count) || 0,
      estimateMB: Number(entry.estimateMB) || 0,
      lastAt: entry.lastAt || null,
      version: entry.version || null,
    })
  }
  rows.sort((a, b) => b.estimateMB - a.estimateMB || b.count - a.count)
  return rows.slice(0, Math.max(1, limit))
}

/**
 * Top5 v2: combine measured module-load MB, operation estimates and the RSS of
 * child processes attributed to a plugin. The ranking is smoothed and stable:
 * an item only changes rank when its score moved by more than 20%.
 */
export function rankTopPlugins({ operations = [], budgets = {}, rss = {}, previous = {}, alpha = 0.35, limit = 5 } = {}) {
  const rows = new Map()
  for (const row of operations) {
    rows.set(row.target, { target: row.target, operationMB: Number(row.estimateMB) || 0, count: Number(row.count) || 0, version: row.version || null, source: row.source || 'estimate', measuredAt: row.measuredAt || null })
  }
  for (const [target, entry] of Object.entries(budgets)) {
    if (!entry || entry.error || entry.moduleLoadMB === undefined) continue
    const current = rows.get(target) || { target, operationMB: 0, count: 0, version: entry.version || null, source: 'measured', measuredAt: entry.measuredAt || null }
    current.moduleLoadMB = Number(entry.moduleLoadMB) || 0
    current.source = 'measured'
    current.measuredAt = entry.measuredAt || current.measuredAt
    current.version = entry.version || current.version
    rows.set(target, current)
  }
  for (const [target, bytes] of Object.entries(rss)) {
    const current = rows.get(target) || { target, operationMB: 0, count: 0, version: null, source: 'estimate', measuredAt: null }
    const previousRss = Number(previous[target] && previous[target].rssMB) || 0
    current.rssMB = previousRss > 0 ? Math.round(previousRss * (1 - alpha) + (Number(bytes) / (1024 * 1024)) * alpha) : Math.round((Number(bytes) || 0) / (1024 * 1024))
    rows.set(target, current)
  }
  const ranked = [...rows.values()].map((row) => ({
    ...row,
    rssMB: row.rssMB || 0,
    score: Math.round((row.moduleLoadMB || row.operationMB || 0) + (row.rssMB || 0)),
  }))
  ranked.sort((a, b) => b.score - a.score)
  const changed = ranked.some((row) => {
    const before = previous[row.target]
    if (!before || !Number.isFinite(before.score)) return true
    const delta = Math.abs(row.score - before.score)
    return delta / Math.max(1, before.score) > 0.2
  })
  const next = {}
  for (const row of ranked) next[row.target] = { score: row.score, rssMB: row.rssMB }
  if (!changed) {
    // keep the last order while scores are stable
    const order = Object.keys(previous)
    ranked.sort((a, b) => order.indexOf(a.target) - order.indexOf(b.target))
  }
  return { rows: ranked.slice(0, Math.max(1, limit)), nextRanking: next }
}

/** Named byte formatting used by the panel/tool text. */
export function formatBytes(bytes) {
  const value = Number(bytes)
  if (!Number.isFinite(value) || value < 0) return '—'
  if (value >= GB) return `${(value / GB).toFixed(2)} GB`
  return `${Math.round(value / MB)} MB`
}

/**
 * Drop accounting rows for bundles that are no longer enabled, or whose version
 * changed since the budget was measured. Keeps Top5/budgets honest after
 * uninstalls and upgrades (state otherwise keeps stale targets forever).
 */
export function prunePluginState(state, rows) {
  const list = Array.isArray(rows) ? rows : []
  const enabled = new Set(list.filter((row) => row.enabled).map((row) => row.name))
  const versions = new Map(list.filter((row) => row.enabled).map((row) => [row.name, String(row.version || '')]))
  const removed = { operations: [], budgets: [], ranking: [], rss: [] }

  if (state.operations instanceof Map) {
    for (const target of [...state.operations.keys()]) {
      if (!enabled.has(target)) {
        state.operations.delete(target)
        removed.operations.push(target)
      }
    }
  }
  for (const target of Object.keys(state.budgets || {})) {
    const entry = state.budgets[target]
    const measuredVersion = entry && entry.version ? String(entry.version) : ''
    if (!enabled.has(target) || (measuredVersion && versions.get(target) !== measuredVersion)) {
      delete state.budgets[target]
      removed.budgets.push(target)
    }
  }
  for (const target of Object.keys(state.ranking || {})) {
    if (!enabled.has(target)) {
      delete state.ranking[target]
      removed.ranking.push(target)
    }
  }
  for (const target of Object.keys(state.rss || {})) {
    if (!enabled.has(target)) {
      delete state.rss[target]
      removed.rss.push(target)
    }
  }
  return removed
}
