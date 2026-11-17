import { existsSync, readdirSync, statSync } from 'node:fs'
import { describeCli, resolveCli, resolveDataDir, runCli } from './cli.js'
import { parseLevels } from './levels.js'

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
    return { dir: dataDir || null, exists: false, integrity: 'none', records: null, detail: '', files: 0 }
  }
  let files = 0
  try {
    files = readdirSync(dataDir).length
  } catch {
    files = 0
  }
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
    }
  }
  return {
    dir: dataDir,
    exists: true,
    integrity: 'failed',
    records: null,
    detail: firstLines(text, 3) || (result.timedOut ? 'verify timed out' : 'verify failed'),
    files,
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
    cli,
    levels,
    ledger,
  }
}

/** Bounded human summary used by tool results. */
export function statusSummary(status) {
  const cliLine = status.cli.found
    ? `aiprison ${status.cli.version || '?'}（${status.cli.source}: ${status.cli.path}）`
    : `CLI 不可用：${status.cli.error || 'unknown'}`
  const ledgerLine = status.ledger.exists
    ? `账本 ${status.ledger.dir}：${status.ledger.integrity === 'ok' ? `INTEGRITY OK（${status.ledger.records ?? '?'} 条记录）` : `校验失败（${status.ledger.detail}）`}`
    : `账本 ${status.ledger.dir}：尚未初始化`
  return [cliLine, ledgerLine, `分级矩阵：${status.levels.length} 级`].join('\n')
}
