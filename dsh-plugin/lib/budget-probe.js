import { spawn } from 'node:child_process'
import { fileURLToPath } from 'node:url'

const CHILD = fileURLToPath(new URL('./budget-child.mjs', import.meta.url))

/**
 * Measure a plugin package's module-load heap cost in an isolated child process.
 * The child runs with --expose-gc, resolves the package from the profile, imports
 * its entry module, forces GC and reports the retained delta. This is the
 * "启动内存" budget that feeds the 300/500 MB per-plugin thresholds.
 */
export function probeBudget({ profileDir, packageName, timeoutMs = 25_000 } = {}) {
  return new Promise((resolve) => {
    const nodeExe = process.env.DSH_PROBE_NODE || process.execPath
    const installDir = process.env.DSH_INSTALL_DIR || (typeof process.argv[2] === 'string' && process.argv[2].endsWith('package.json') === false ? process.argv[2] : '')
    const env = { ...process.env }
    if (process.versions.electron) env.ELECTRON_RUN_AS_NODE = '1'
    delete env.NODE_OPTIONS
    const child = spawn(nodeExe, ['--expose-gc', CHILD, profileDir, packageName, installDir || ''], {
      env,
      windowsHide: true,
      stdio: ['ignore', 'pipe', 'pipe'],
    })
    let stdout = ''
    let stderr = ''
    let settled = false
    const finish = (value) => {
      if (settled) return
      settled = true
      clearTimeout(timer)
      try {
        child.kill()
      } catch {
        // already gone
      }
      resolve(value)
    }
    const timer = setTimeout(() => finish({ ok: false, error: 'probe-timeout' }), timeoutMs)
    child.stdout.on('data', (chunk) => {
      stdout += String(chunk)
    })
    child.stderr.on('data', (chunk) => {
      stderr = (stderr + String(chunk)).slice(-2000)
    })
    child.on('error', (error) => finish({ ok: false, error: String(error && error.message) }))
    child.on('exit', (code) => {
      const line = stdout.trim().split('\n').filter(Boolean).pop()
      if (!line) return finish({ ok: false, error: `probe-exit-${code}: ${stderr.slice(0, 300)}` })
      try {
        const parsed = JSON.parse(line)
        finish(parsed)
      } catch {
        finish({ ok: false, error: `probe-bad-json: ${line.slice(0, 300)}` })
      }
    })
  })
}
