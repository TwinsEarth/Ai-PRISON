import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'

/**
 * Shared constants, path resolution and low-level readers for the watchdog.
 * Keeping them in one place lets the runtime, crash reader and budget probe
 * agree on where DSH keeps its profile, logs and operation journal.
 */
export const SAMPLE_MS = 5_000
export const FLUSH_MS = 30_000
export const CRASH_HEAD_BYTES = 256 * 1024
export const BASELINE_DELAY_MS = 60_000
export const PAUSE_GRACE_MS = 10 * 60_000
export const CRASH_WINDOW_MS = 10 * 60_000
export const MAX_JOURNAL_RECORDS = 4000

/** Bundles the watchdog must never quarantine or disable. */
export const EXEMPT_TARGETS = new Set([
  'dsh-base',
  'dsh-web-app',
  '@deepseek-ai/dsh-base',
  '@deepseek-ai/dsh-web-app',
  '@twinsearth/ai-prison-dsh-plugin',
])

export function resolveProfileDir() {
  const fromEnv = typeof process.env.DSH_PROFILE_DIR === 'string' && process.env.DSH_PROFILE_DIR.trim()
  if (fromEnv) return fromEnv
  const name = typeof process.env.DSH_PROFILE === 'string' && process.env.DSH_PROFILE.trim() ? process.env.DSH_PROFILE.trim() : 'desktop'
  return path.join(os.homedir(), '.dsh', 'profiles', name)
}

export function resolveLogsDir() {
  const roaming = process.env.APPDATA || path.join(os.homedir(), 'AppData', 'Roaming')
  return path.join(roaming, '@deepseek-ai', 'dsh-desktop', 'logs')
}

export function resolveJournalDir() {
  const roaming = process.env.APPDATA || path.join(os.homedir(), 'AppData', 'Roaming')
  return path.join(roaming, '@deepseek-ai', 'dsh-desktop', 'plugin-operations')
}

/** Read the first bytes of a file without loading the whole crash log. */
export function readHead(file, bytes) {
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

/** Last records of the plugin-manager operation journal (both files). */
export function readJournalRecords(journalDir) {
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

/** Info/warn facade that tolerates a missing or partial cordis logger. */
export function createLogger(logger = {}) {
  return {
    info: (...args) => (typeof logger.info === 'function' ? logger.info(...args) : undefined),
    warn: (...args) => (typeof logger.warn === 'function' ? logger.warn(...args) : undefined),
  }
}
