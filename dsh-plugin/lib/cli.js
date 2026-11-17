import { spawn } from 'node:child_process'
import { existsSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

/** Package root: the bundle directory that owns bin/, lib/ and index.js. */
export const PACKAGE_ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')

export const PLATFORM_KEYS = { win32: 'windows', darwin: 'macos', linux: 'linux' }
export const ARCH_KEYS = { x64: 'amd64', arm64: 'arm64' }

/** Platforms the release bundles; keep in sync with scripts/build-binaries.mjs. */
export const BUNDLED_TARGETS = [
  'windows-amd64',
  'windows-arm64',
  'macos-amd64',
  'macos-arm64',
  'linux-amd64',
  'linux-arm64',
]

const MAX_CAPTURE = 512 * 1024

function trimEnv(value) {
  return typeof value === 'string' && value.trim() !== '' ? value.trim() : undefined
}

function expandHome(value) {
  if (typeof value !== 'string') return undefined
  const text = value.trim()
  if (text === '') return undefined
  if (text === '~') return path.join(process.env.HOME || process.env.USERPROFILE || '')
  if (text.startsWith('~/') || text.startsWith('~\\')) {
    return path.join(process.env.HOME || process.env.USERPROFILE || '', text.slice(2))
  }
  return text
}

/** Path of the bundled CLI for one platform, when that build ships. */
export function bundledCliPath({ platform = process.platform, arch = process.arch, root = PACKAGE_ROOT } = {}) {
  const dir = `${PLATFORM_KEYS[platform] || platform}-${ARCH_KEYS[arch] || arch}`
  const file = path.join(root, 'bin', dir, platform === 'win32' ? 'aiprison.exe' : 'aiprison')
  return existsSync(file) ? file : undefined
}

/** Search PATH entries for a native aiprison executable (`aiprison`, not a shim). */
export function pathCliPath({ env = process.env, platform = process.platform } = {}) {
  const name = platform === 'win32' ? 'aiprison.exe' : 'aiprison'
  const entries = String(env.PATH || env.Path || '')
    .split(platform === 'win32' ? ';' : ':')
    .map((entry) => entry.trim())
    .filter(Boolean)
  for (const entry of entries) {
    const candidate = path.join(entry, name)
    if (existsSync(candidate)) return candidate
  }
  return undefined
}

/**
 * Resolve the CLI the plugin should run, in priority order: explicit config,
 * AIPRISON_BIN, the bundled build, then PATH. Returns the path plus the source
 * label used by tool results and the panel.
 */
export function resolveCli({ config = {}, env = process.env, platform = process.platform, arch = process.arch, root = PACKAGE_ROOT } = {}) {
  const fromConfig = expandHome(config.cliPath)
  if (fromConfig) {
    if (existsSync(fromConfig)) return { path: fromConfig, source: 'config' }
    return { missing: `配置的 cliPath 不存在：${fromConfig}`, source: 'config' }
  }
  const fromEnv = expandHome(trimEnv(env.AIPRISON_BIN))
  if (fromEnv) {
    if (existsSync(fromEnv)) return { path: fromEnv, source: 'AIPRISON_BIN' }
    return { missing: `AIPRISON_BIN 指向的文件不存在：${fromEnv}`, source: 'env' }
  }
  const bundled = bundledCliPath({ platform, arch, root })
  if (bundled) return { path: bundled, source: 'bundled' }
  const fromPath = pathCliPath({ env, platform })
  if (fromPath) return { path: fromPath, source: 'PATH' }
  return {
    missing:
      `没有找到 aiprison 可执行文件（本平台 ${platform}-${arch} 没有内置构建）。` +
      '可设置 AIPRISON_BIN、在插件配置里填 cliPath，或把 aiprison 放进 PATH。',
    source: 'none',
  }
}

function truncate(text) {
  if (text.length <= MAX_CAPTURE) return text
  return `${text.slice(0, MAX_CAPTURE / 2)}\n…[输出被截断]…\n${text.slice(-MAX_CAPTURE / 2)}`
}

/**
 * Run the CLI and never reject: a non-zero exit, a spawn failure and a timeout
 * all come back as data so a tool can report them without ending the turn.
 */
export function runCli(args, { bin, cwd, timeoutMs = 120_000, env = process.env, signal } = {}) {
  return new Promise((resolve) => {
    if (typeof bin !== 'string' || bin === '') {
      resolve({ ok: false, exitCode: null, stdout: '', stderr: '', error: 'no binary', timedOut: false })
      return
    }
    const options = { cwd: cwd || process.cwd(), env, windowsHide: true }
    let child
    try {
      child = spawn(bin, args, options)
    } catch (error) {
      resolve({ ok: false, exitCode: null, stdout: '', stderr: '', error: String(error && error.message), timedOut: false })
      return
    }
    let stdout = ''
    let stderr = ''
    let timedOut = false
    let spawnError
    const timer = setTimeout(() => {
      timedOut = true
      try {
        child.kill('SIGKILL')
      } catch {
        // The exit handler below still settles the promise.
      }
    }, Math.max(1_000, Number(timeoutMs) || 120_000))
    const onAbort = () => {
      try {
        child.kill('SIGKILL')
      } catch {
        // ignore
      }
    }
    if (signal && typeof signal.addEventListener === 'function') {
      if (signal.aborted) onAbort()
      else signal.addEventListener('abort', onAbort, { once: true })
    }
    child.stdout?.on('data', (chunk) => {
      stdout = truncate(stdout + chunk.toString())
    })
    child.stderr?.on('data', (chunk) => {
      stderr = truncate(stderr + chunk.toString())
    })
    child.on('error', (error) => {
      spawnError = String((error && error.message) || error)
    })
    child.on('close', (code, sig) => {
      clearTimeout(timer)
      if (signal && typeof signal.removeEventListener === 'function') signal.removeEventListener('abort', onAbort)
      resolve({
        ok: code === 0 && !timedOut && !spawnError,
        exitCode: code,
        signal: sig || undefined,
        stdout: stdout.trimEnd(),
        stderr: stderr.trimEnd(),
        error: spawnError,
        timedOut,
        bin,
      })
    })
  })
}

/** Resolve the CLI and read `aiprison version -v`; used by tools and the panel. */
export async function describeCli(options = {}) {
  const resolved = resolveCli(options)
  if (!resolved.path) return { found: false, source: resolved.source, error: resolved.missing, path: null, version: null, commit: null }
  const probe = await runCli(['version', '-v'], { bin: resolved.path, cwd: options.cwd, timeoutMs: options.timeoutMs })
  const text = `${probe.stdout}\n${probe.stderr}`
  const versionMatch = text.match(/aiprison\s+([0-9][^\s]*)/)
  const commitMatch = text.match(/commit:\s*([^\s]+)/)
  return {
    found: probe.ok,
    source: resolved.source,
    path: resolved.path,
    version: versionMatch ? versionMatch[1] : null,
    commit: commitMatch ? commitMatch[1] : null,
    error: probe.ok ? null : `${probe.stderr || probe.stdout || 'version probe failed'}`,
  }
}

/**
 * Default durable ledger directory: the plugin config when set, else
 * `<working directory>/.ai-prison` so the ledger lives beside the project it
 * audits instead of in a shared home directory.
 */
export function resolveDataDir({ config = {}, cwd } = {}) {
  const configured = expandHome(config.dataDir)
  if (configured) return configured
  return path.join(cwd || process.cwd(), '.ai-prison')
}
