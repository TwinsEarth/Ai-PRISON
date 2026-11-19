import { existsSync, readdirSync } from 'node:fs'
import { describeCli, resolveCli, resolveDataDir, runCli } from './cli.js'
import { parseLevels } from './levels.js'
import { listSegments } from './ledger.js'
import { PLUGIN_VERSION } from './version.js'

const MAX_DETAIL = 600

function firstLines(text, count = 3) {
  return String(text || '')
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean)
    .slice(0, count)
    .join(' · ')
    .slice(0, MAX_DETAIL)
}

/** Read the durable ledger's integrity through `aiprison verify`. */
export async function inspectLedger({ bin, dataDir, timeoutMs } = {}) {
  if (!dataDir || !existsSync(dataDir)) {
    return { dir: dataDir || null, exists: false, integrity: 'none', records: null, detail: '', files: 0, segments: 0, canDemo: true }
  }
  let files = 0
  try {
    files = readdirSync(dataDir).length
  } catch {
    files = 0
  }
  const segments = listSegments(dataDir).length
  const result = await runCli(['--data-dir', dataDir, 'verify'], { bin, timeoutMs })
  const text = result.ok ? result.stdout : `${result.stdout}\n${result.stderr}`
  if (result.ok) {
    const records = text.match(/(\d+)\s+records?/i)
    return {
      dir: dataDir,
      exists: true,
      integrity: 'ok',
      records: records ? Number(records[1]) : null,
      detail: firstLines(text, 1),
      files,
      segments,
      canDemo: segments === 0,
    }
  }
  return {
    dir: dataDir,
    exists: true,
    integrity: 'failed',
    records: null,
    detail: firstLines(text, 3) || (result.timedOut ? 'verify timed out' : 'verify failed'),
    files,
    segments,
    canDemo: segments === 0,
  }
}

/** Everything the panel and the status tool show, in one read-only snapshot. */
export async function buildStatus({ config = {}, cwd, timeoutMs = 120_000, root } = {}) {
  const resolved = resolveCli({ config, root })
  const cli = await describeCli({ config, cwd, timeoutMs, root })
  let levels = []
  if (resolved.path) {
    const levelsRun = await runCli(['levels'], { bin: resolved.path, cwd, timeoutMs })
    if (levelsRun.ok) levels = parseLevels(levelsRun.stdout)
  }
  const dataDir = resolveDataDir({ config, cwd })
  const ledger = await inspectLedger({ bin: resolved.path, dataDir, timeoutMs })
  return {
    ok: resolved.path !== undefined,
    generatedAt: new Date().toISOString(),
    cwd: cwd || null,
    platform: `${process.platform}-${process.arch}`,
    pluginVersion: PLUGIN_VERSION,
    cli,
    levels,
    ledger,
  }
}

/** Bounded human summary used by tool results. */
export function statusSummary(status) {
  const cliLine = status.cli.found
    ? `AI Prison v${status.pluginVersion || '?'} · aiprison ${status.cli.version || '?'}（${status.cli.source}: ${status.cli.path}）`
    : `CLI 不可用：${status.cli.error || 'unknown'}`
  const ledgerLine = status.ledger.exists
    ? `账本 ${status.ledger.dir}：${status.ledger.integrity === 'ok' ? `INTEGRITY OK（${status.ledger.records ?? '?'} 条记录）` : `校验失败（${status.ledger.detail}）`}`
    : `账本 ${status.ledger.dir}：尚未初始化`
  return [cliLine, ledgerLine, `分级矩阵：${status.levels.length} 级`].join('\n')
}

/** One-line outcome for a panel action (`demo` or `drill`). */
export function summarizeRun(action, result) {
  if (result && result.timedOut) return `超时：${action} 超过时间上限`
  const lines = `${(result && result.stdout) || ''}\n${(result && result.stderr) || ''}`
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean)
  const picked = lines.filter((line) => /OVERALL|已落盘|INTEGRITY|记录数|段数|最终运行状态|恢复令/.test(line)).slice(-4)
  const detail = (picked.length > 0 ? picked : lines.slice(-2)).join(' · ').slice(0, 500)
  if (result && result.ok) return detail || `${action} 完成`
  return `失败（exit ${result ? result.exitCode : '?'}）：${detail || 'no output'}`
}
