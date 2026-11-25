import fs from 'node:fs'
import path from 'node:path'

/** Ledger segment files written by the CLI: `seg-<10-digit start seq>.log`. */
export const SEGMENT_PATTERN = /^seg-\d+\.log$/

/** Segments currently present in a ledger directory (newest name last). */
export function listSegments(dataDir) {
  if (!dataDir) return []
  let entries
  try {
    entries = fs.readdirSync(dataDir, { withFileTypes: true })
  } catch {
    return []
  }
  return entries
    .filter((entry) => entry.isFile() && SEGMENT_PATTERN.test(entry.name))
    .map((entry) => {
      let bytes = 0
      try {
        bytes = fs.statSync(path.join(dataDir, entry.name)).size
      } catch {
        bytes = 0
      }
      return { name: entry.name, bytes }
    })
    .sort((a, b) => a.name.localeCompare(b.name))
}

/**
 * Move only the CLI's own segment files out of the active ledger into
 * `<dataDir>/.trash/<timestamp>/`. Nothing else is touched: no recursion, no
 * directories, no files that the CLI did not name. The move is reversible.
 */
export function resetLedger(dataDir) {
  if (!dataDir) return { ok: false, removed: [], error: 'no ledger directory' }
  const resolved = path.resolve(dataDir)
  if (path.dirname(resolved) === resolved) {
    return { ok: false, removed: [], error: 'refusing to clear a filesystem root' }
  }
  const segments = listSegments(resolved)
  if (segments.length === 0) {
    return { ok: true, removed: [], failed: [], trashDir: null }
  }
  const stamp = new Date().toISOString().replace(/[:.]/g, '-')
  const trashDir = path.join(resolved, '.trash', stamp)
  try {
    fs.mkdirSync(trashDir, { recursive: true })
  } catch (error) {
    return { ok: false, removed: [], error: String((error && error.message) || error), trashDir }
  }
  const removed = []
  const failed = []
  for (const segment of segments) {
    try {
      fs.renameSync(path.join(resolved, segment.name), path.join(trashDir, segment.name))
      removed.push(segment.name)
    } catch (error) {
      failed.push(`${segment.name}: ${String((error && error.message) || error)}`)
    }
  }
  return {
    ok: failed.length === 0,
    removed,
    failed,
    trashDir,
    error: failed.length > 0 ? failed.join('; ') : undefined,
  }
}
