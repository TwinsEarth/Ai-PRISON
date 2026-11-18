import fs from 'node:fs'
import path from 'node:path'
import crypto from 'node:crypto'

/**
 * Hash watch for the profile files a plugin must never change behind the plugin
 * manager's back. A drift outside a plugin-manager operation window is treated
 * as tampering: the original bytes are restored (policy "restore") and the
 * watchdog raises an L1 plan for the last installed/updated plugin.
 */
export const CRITICAL_FILES = ['package.json', 'cordis.patch.yml', 'cordis.yml', 'pnpm-workspace.yaml', 'pnpm-lock.yaml']
export const OPERATION_GRACE_MS = 4000
export const STARTUP_GRACE_MS = 15000
export const DEFAULT_INTERVAL_MS = 30000

export function sha256(buffer) {
  return crypto.createHash('sha256').update(buffer).digest('hex')
}

function readIfExists(file) {
  try {
    return fs.readFileSync(file)
  } catch {
    return null
  }
}

/** Snapshot the critical profile files, keeping the bytes needed for restore. */
export function snapshotCriticalFiles(profileDir) {
  const files = []
  const contents = new Map()
  for (const rel of CRITICAL_FILES) {
    const file = path.join(profileDir, rel)
    const bytes = readIfExists(file)
    contents.set(rel, bytes)
    let mtimeMs = 0
    try { mtimeMs = fs.statSync(file).mtimeMs } catch { mtimeMs = 0 }
    files.push({ file: rel, exists: bytes !== null, hash: bytes === null ? null : sha256(bytes), size: bytes === null ? 0 : bytes.length, mtimeMs })
  }
  return { files, contents }
}

/** Difference between two snapshot file lists. */
export function diffSnapshots(previous, next) {
  const before = new Map((previous || []).map((entry) => [entry.file, entry]))
  const diffs = []
  for (const entry of next || []) {
    const old = before.get(entry.file)
    if (!old) continue
    if (!old.exists && entry.exists) diffs.push({ file: entry.file, kind: 'created', mtimeMs: entry.mtimeMs })
    else if (old.exists && !entry.exists) diffs.push({ file: entry.file, kind: 'deleted', mtimeMs: entry.mtimeMs })
    else if (old.exists && entry.exists && old.hash !== entry.hash) diffs.push({ file: entry.file, kind: 'modified', mtimeMs: entry.mtimeMs })
  }
  return diffs
}

/** Persist the tampered bytes next to the watchdog state for evidence. */
export function backupTampered(dataDir, profileDir, diffs) {
  if (!Array.isArray(diffs) || diffs.length === 0) return []
  const stamp = new Date().toISOString().replace(/[:.]/g, '-')
  const dir = path.join(dataDir, 'tampered', stamp)
  const written = []
  try {
    fs.mkdirSync(dir, { recursive: true })
  } catch {
    return written
  }
  for (const diff of diffs) {
    const bytes = readIfExists(path.join(profileDir, diff.file))
    if (bytes === null) continue
    const target = path.join(dir, diff.file.replace(/[\\/]/g, '__'))
    try {
      fs.writeFileSync(target, bytes)
      written.push(target)
    } catch {
      // evidence is best effort
    }
  }
  return written
}

/** Restore the baseline bytes for modified/deleted files. Created files are not removed. */
export function restoreBaseline(profileDir, baselineContents, diffs) {
  const restored = []
  for (const diff of diffs || []) {
    if (diff.kind === 'created') continue
    const bytes = baselineContents instanceof Map ? baselineContents.get(diff.file) : null
    if (!bytes) continue
    try {
      fs.mkdirSync(path.dirname(path.join(profileDir, diff.file)), { recursive: true })
      fs.writeFileSync(path.join(profileDir, diff.file), bytes)
      restored.push(diff.file)
    } catch {
      // restore is best effort; the incident still reports the drift
    }
  }
  return restored
}

/**
 * Create the profile tamper monitor.
 * `shouldIgnore()` lets the caller suppress legitimate write windows (plugin
 * manager operations); `onTamper({diffs, restored, files})` fires once per
 * detected drift.
 */
export function createTamperMonitor({
  profileDir,
  dataDir,
  policy = 'restore',
  graceMs = STARTUP_GRACE_MS,
  intervalMs = DEFAULT_INTERVAL_MS,
  shouldIgnore,
  isWithinOperationWindow,
  onTamper,
  log,
} = {}) {
  const base = snapshotCriticalFiles(profileDir)
  let baseline = base
  let baselineAt = Date.now()
  let timer = null
  const status = {
    enabled: true,
    policy,
    files: base.files.map((entry) => entry.file),
    baselineAt: new Date(baselineAt).toISOString(),
    lastCheckAt: null,
    lastDiff: [],
    restored: [],
    skipped: null,
    incidents: 0,
  }

  function checkNow({ force = false } = {}) {
    const next = snapshotCriticalFiles(profileDir)
    const diffs = diffSnapshots(baseline.files, next.files)
    status.lastCheckAt = new Date().toISOString()
    if (diffs.length === 0) {
      baseline = next
      baselineAt = Date.now()
      status.lastDiff = []
      status.skipped = null
      return { ok: true, diffs: [], restored: [] }
    }
    // A changed file written inside a plugin-manager operation window is a
    // legitimate manager write even when this poll runs after the window closed.
    const withinWindow = (mtimeMs) => typeof isWithinOperationWindow === 'function' && isWithinOperationWindow(mtimeMs)
    const suspicious = force ? diffs : diffs.filter((diff) => !withinWindow(diff.mtimeMs))
    const startup = !force && Date.now() - baselineAt < graceMs
    const ignored = !force && typeof shouldIgnore === 'function' && shouldIgnore()
    if (startup || ignored || suspicious.length === 0) {
      baseline = next
      baselineAt = Date.now()
      status.skipped = startup ? 'startup' : 'operation-window'
      status.lastDiff = []
      if (typeof log === 'function') log(`tamper monitor: ${diffs.map((d) => d.file).join(', ')} ${status.skipped}（视为正常写入）`)
      return { ok: true, diffs: [], restored: [], skipped: status.skipped }
    }
    const backup = backupTampered(dataDir, profileDir, suspicious)
    const restored = policy === 'restore' ? restoreBaseline(profileDir, baseline.contents, suspicious) : []
    status.lastDiff = suspicious
    status.restored = restored
    status.incidents += 1
    // Re-baseline on the post-restore state so one drift yields one alert.
    baseline = snapshotCriticalFiles(profileDir)
    baselineAt = Date.now()
    if (backup.length > 0 && typeof log === 'function') log(`tamper monitor: 备份到 ${backup.length} 个文件`)
    if (typeof onTamper === 'function') onTamper({ diffs: suspicious, restored, policy, backup })
    return { ok: true, diffs, restored, backup }
  }

  function start() {
    if (timer || !(intervalMs > 0)) return
    timer = setInterval(() => {
      try {
        checkNow()
      } catch {
        // keep polling; a read failure must not stop the monitor
      }
    }, intervalMs)
    if (typeof timer.unref === 'function') timer.unref()
  }

  function stop() {
    if (timer) clearInterval(timer)
    timer = null
  }

  return { checkNow, start, stop, status: () => ({ ...status }), snapshot: () => baseline.files }
}
