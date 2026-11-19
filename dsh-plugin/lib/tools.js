import path from 'node:path'
import { resolveCli, resolveDataDir, runCli } from './cli.js'
import { loadDefineTool } from './harness.js'
import { buildStatus, statusSummary } from './status.js'
import { watchdogSnapshot } from './watchdog-runtime.js'
import { formatBytes } from './watchdog-policy.js'

const MAX_TEXT = 4_000
const TAIL_LINES = 60

export function bounded(result, { lines = TAIL_LINES, max = MAX_TEXT } = {}) {
  const text = String(result || '')
  const rows = text.split(/\r?\n/)
  const tail = rows.slice(-lines).join('\n')
  const clipped = rows.length > lines ? `…[前 ${rows.length - lines} 行省略]…\n${tail}` : tail
  return clipped.length > max ? `${clipped.slice(-max)}…[截断]` : clipped
}

/**
 * The session a tool call belongs to. `workingDirectory.get` expects a Session,
 * not the Agent id the execution context carries, so map it through `sessions`.
 */
export function resolveToolCwd(ctx, exec) {
  const get = (key) => (ctx && typeof ctx.get === 'function' ? ctx.get(key) : undefined)
  try {
    const sessions = get('sessions')
    const workingDirectory = get('workingDirectory')
    const agent = exec && exec.agent
    let session
    if (sessions && typeof sessions.get === 'function' && agent && agent.id) {
      session = sessions.get(agent.id)
    }
    if (session && workingDirectory && typeof workingDirectory.get === 'function') {
      try {
        const dir = workingDirectory.get(session)
        if (typeof dir === 'string' && dir) return dir
      } catch {
        // Fall through to the session's original directory.
      }
    }
    const headerCwd = session && session.header ? session.header.cwd : undefined
    if (typeof headerCwd === 'string' && headerCwd) return headerCwd
    if (workingDirectory && typeof workingDirectory.defaultDirectory === 'string' && workingDirectory.defaultDirectory) {
      return workingDirectory.defaultDirectory
    }
  } catch {
    // A profile without sessions or workingDirectory runs the CLI in the host cwd.
  }
  return process.cwd()
}

const cwdOf = (ctx, exec) => resolveToolCwd(ctx, exec)

function parametersOf(config) {
  const timeout = Number(config && config.timeoutMs) > 0 ? Number(config.timeoutMs) : 120_000
  return timeout
}

/**
 * Build the argument vector for one CLI invocation. `--data-dir` is accepted
 * before the subcommand, which is the CLI's documented global flag position.
 */
async function invoke({ ctx, config, exec, subcommand, args = [], dataDir: explicitDataDir, timeoutMs }) {
  const resolved = resolveCli({ config })
  if (!resolved.path) {
    return {
      payload: {
        ok: false,
        reason: 'cli-missing',
        error: resolved.missing,
        hint: '设置 AIPRISON_BIN、在插件配置里填 cliPath，或把 aiprison 放进 PATH。',
      },
      ok: false,
    }
  }
  const cwd = cwdOf(ctx, exec)
  const dataDir = explicitDataDir ? path.resolve(cwd, explicitDataDir) : resolveDataDir({ config, cwd })
  const result = await runCli(['--data-dir', dataDir, subcommand, ...args], {
    bin: resolved.path,
    cwd,
    timeoutMs: Number(timeoutMs) > 0 ? Number(timeoutMs) : parametersOf(config),
    signal: exec && exec.signal,
  })
  const payload = {
    ok: result.ok,
    command: subcommand,
    dataDir,
    exitCode: result.exitCode,
    timedOut: result.timedOut === true,
    stdout: bounded(result.stdout),
    stderr: bounded(result.stderr),
  }
  return { payload, ok: result.ok, resolved, dataDir, result }
}

function toolResult(payload) {
  return JSON.stringify(payload, null, 2)
}

/**
 * Register one tool definition.
 *
 * The official Host API is `ctx.tools.register(definition)` and accepts a plain
 * ToolDefinition (name/description/parameters/output/execute). `defineTool` from
 * @deepseek-ai/dsh-tools is only a compatibility fallback for older harness
 * builds; resolving it by guessing an installation path must never be required.
 */
export function registerToolDefinition(tools, definition, { defineTool } = {}) {
  try {
    tools.register(definition)
    return 'plain'
  } catch (error) {
    if (typeof defineTool !== 'function') throw error
    tools.register(defineTool(definition))
    return 'fallback'
  }
}

export async function installTools({ ctx, config = {}, logger } = {}) {
  if (!ctx || typeof ctx.inject !== 'function') {
    logger?.warn?.('ctx.inject 不可用，跳过 aiprison_* 工具注册')
    return false
  }
  // Official API first: ctx.tools.register accepts a plain ToolDefinition.
  // defineTool is loaded lazily only as a fallback for older harness builds.
  let defineTool
  try {
    const loaded = await loadDefineTool()
    defineTool = loaded && loaded.defineTool
  } catch {
    defineTool = undefined
  }
  const stringOutput = {
    schema: { type: 'string' },
    render: (_args, value) => [{ type: 'text', text: String(value) }],
  }
  const dataDirParameter = {
    type: 'string',
    description: 'Durable ledger directory. Default: the plugin config, else <session working directory>/.ai-prison.',
  }

  ctx.inject(['tools'], (child) => {
    const register = (definition) => {
      try {
        const mode = registerToolDefinition(child.tools, definition, { defineTool })
        if (mode === 'fallback') logger?.info?.(`工具 ${definition.name} 使用 defineTool 兼容路径注册`)
      } catch (error) {
        logger?.warn?.(`注册工具 ${definition.name} 失败：${String(error && error.message)}`)
      }
    }

    register({
      name: 'ai_prison_levels',
      description:
        'Print the L0–L5 grading matrix of AI Prison: each level code, name, duration class and the capabilities it deprives. Read-only; no ledger is touched.',
      parameters: {
        data_dir: { ...dataDirParameter, description: 'Unused by this command; accepted for symmetry with the ledger tools.' },
      },
      output: stringOutput,
      async execute(args, exec) {
        const outcome = await invoke({ ctx, config, exec, subcommand: 'levels', dataDir: args?.data_dir })
        if (!outcome.ok) return toolResult(outcome.payload)
        return toolResult({ ok: true, command: 'levels', matrix: outcome.payload.stdout })
      },
    })

    register({
      name: 'ai_prison_status',
      description:
        'Read-only snapshot of AI Prison in this workspace: resolved CLI (path, source, version), the L0–L5 matrix, and the durable ledger directory with its audit-chain integrity and record count. The panel shows the same data.',
      parameters: {
        data_dir: dataDirParameter,
      },
      output: stringOutput,
      async execute(args, exec) {
        const cwd = cwdOf(ctx, exec)
        const dataDir = args?.data_dir ? path.resolve(cwd, args.data_dir) : undefined
        if (dataDir) {
          const status = await buildStatus({ config: { ...config, dataDir }, cwd, root: undefined })
          return toolResult({ ok: true, summary: statusSummary(status), status })
        }
        const status = await buildStatus({ config, cwd })
        return toolResult({ ok: true, summary: statusSummary(status), status })
      },
    })

    register({
      name: 'ai_prison_watchdog_status',
      description:
        'Read the AI Prison watchdog snapshot (P1 enforce mode): heap waterline (warn 3 GB / pause 3.9 GB / freeze 5 GB on a 6 GB host), per-plugin hot-operation counts and heap estimates, the plugin prison with quarantined targets and release state, the memory Top5, dry-run crash escalation plans (L1-L3) and recent incidents. Enforcement auto-closes a plugin after more than five hot operations in one run; the tool itself never changes a plugin.',
      parameters: {},
      output: stringOutput,
      async execute() {
        const snapshot = watchdogSnapshot()
        if (!snapshot.ok) {
          return toolResult({
            ok: false,
            mode: 'disabled',
            hint: 'watchdog 行未启用：在插件配置里启用 ai-prison-watchdog 后重启即可开始观察。',
          })
        }
        const heap = snapshot.heap || {}
        return toolResult({
          ok: true,
          mode: snapshot.mode,
          dataDir: snapshot.dataDir,
          degraded: snapshot.degraded,
          waterline: snapshot.waterline,
          heap: {
            used: formatBytes(heap.heapUsed),
            total: formatBytes(heap.heapTotal),
            limit: formatBytes(heap.heapLimit),
            rss: formatBytes(heap.rss),
            heapUsedBytes: heap.heapUsed,
            heapLimitBytes: heap.heapLimit,
          },
          thresholds: snapshot.thresholds
            ? {
                totalWarn: formatBytes(snapshot.thresholds.totalWarn),
                totalPause: formatBytes(snapshot.thresholds.totalPause),
                totalBlock: formatBytes(snapshot.thresholds.totalBlock),
                singleWarn: formatBytes(snapshot.thresholds.singleWarn),
                singleBlock: formatBytes(snapshot.thresholds.singleBlock),
                opWarn: formatBytes(snapshot.thresholds.opWarn),
                opBlock: formatBytes(snapshot.thresholds.opBlock),
              }
            : null,
          enforce: snapshot.enforce,
          strongWarn: snapshot.strongWarn,
          pausedAt: snapshot.pausedAt,
          graceUntil: snapshot.graceUntil,
          frozenAt: snapshot.frozenAt,
          topFive: snapshot.topFive,
          quarantine: snapshot.quarantine,
          plans: snapshot.plans,
          escalation: snapshot.escalation,
          baselineAt: snapshot.baseline ? snapshot.baseline.at : null,
          budgets: snapshot.budgets,
          tamper: snapshot.tamper,
          boot: snapshot.boot,
          heap6g: snapshot.heap6g,
          heap6gRequest: snapshot.heap6gRequest,
          pendingBoot: snapshot.pendingBoot,
          rss: snapshot.rss,
          rssAt: snapshot.rssAt,
          rssError: snapshot.rssError,
          rssStats: snapshot.rssStats,
          rssServiceError: snapshot.rssServiceError,
          operations: snapshot.operations,
          incidents: snapshot.incidents,
          note: snapshot.enforce
            ? 'enforce 模式：热操作 > 5 次自动关闭；3.9GB 暂停需二次确认，5GB 冻结插件操作。'
            : 'observe 模式：仅观察与记录，不自动关闭任何插件。',
        })
      },
    })

    register({
      name: 'ai_prison_demo',
      description:
        'Run the end-to-end governance demo (detect → seven-stage adjudication → layered enforcement → audit → appeal/rehabilitation → recovery) and persist its records into the ledger. Writes to the ledger directory; the demo is entirely in-memory simulation, not real isolation or capability removal.',
      parameters: {
        data_dir: dataDirParameter,
      },
      output: stringOutput,
      async execute(args, exec) {
        const outcome = await invoke({ ctx, config, exec, subcommand: 'demo', dataDir: args?.data_dir })
        if (!outcome.ok) {
          const text = `${outcome.payload.stdout || ''}\n${outcome.payload.stderr || ''}`
          if (/reopened ledger/i.test(text)) {
            outcome.payload.hint =
              '账本非空：demo 只能从空账本开始（CLI 会重放旧账本并拒绝重新分配已吊销的凭据）。可改用 ai_prison_drill，或清空该数据目录下的 seg-*.log 后重试。'
          }
        }
        return toolResult(outcome.payload)
      },
    })

    register({
      name: 'ai_prison_drill',
      description:
        'Run the v1.0.0 recovery drill: walk the reversibility matrix and report PASS/FAIL per assertion plus the overall verdict. Persists drill events into the ledger; all backends stay simulated.',
      parameters: {
        data_dir: dataDirParameter,
      },
      output: stringOutput,
      async execute(args, exec) {
        const outcome = await invoke({ ctx, config, exec, subcommand: 'drill', dataDir: args?.data_dir })
        if (!outcome.ok) {
          const text = `${outcome.payload.stdout || ''}\n${outcome.payload.stderr || ''}`
          if (/reopened ledger|seed allocate/i.test(text)) {
            outcome.payload.hint =
              '账本已被此前的演练写入：再次演练会重放旧事件并拒绝重新分配已吊销的凭据。请清空该数据目录下的 seg-*.log（或换一个空 data_dir）后重试；面板的「运行演练」会自动先清空。'
          }
        }
        return toolResult(outcome.payload)
      },
    })

    register({
      name: 'ai_prison_inspect',
      description:
        'Read-only replay of the ledger: hash chain, agents, sentences, revocations, execution receipts and sealed snapshots. Changes nothing on disk.',
      parameters: {
        data_dir: dataDirParameter,
      },
      output: stringOutput,
      async execute(args, exec) {
        const outcome = await invoke({ ctx, config, exec, subcommand: 'inspect', dataDir: args?.data_dir })
        return toolResult(outcome.payload)
      },
    })

    register({
      name: 'ai_prison_verify',
      description:
        'Integrity-check the ledger: verify every per-record hash link, print INTEGRITY OK with the record count and exit 0, or locate the tamper/torn tail and exit 1. Read-only.',
      parameters: {
        data_dir: dataDirParameter,
      },
      output: stringOutput,
      async execute(args, exec) {
        const outcome = await invoke({ ctx, config, exec, subcommand: 'verify', dataDir: args?.data_dir })
        return toolResult(outcome.payload)
      },
    })

    register({
      name: 'ai_prison_repair_tail',
      description:
        'Remove only a genuine torn (incomplete) tail line from the ledger and refuse hard tampering. Mutating: it truncates the damaged tail so later appends stay replayable. Run ai_prison_verify first and inspect the reported reason.',
      parameters: {
        data_dir: dataDirParameter,
      },
      output: stringOutput,
      async execute(args, exec) {
        const outcome = await invoke({ ctx, config, exec, subcommand: 'repair-tail', dataDir: args?.data_dir })
        return toolResult(outcome.payload)
      },
    })
  })
  return true
}
