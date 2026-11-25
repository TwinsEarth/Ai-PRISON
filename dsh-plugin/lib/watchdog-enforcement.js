import fs from 'node:fs'
import path from 'node:path'
import { attributeCrash, hotOperationTriggered, memoryTierFor, mergeEstimate, operationWeight } from './watchdog-policy.js'
import { remediationFor } from './remediation.js'
import { readJournalRecords } from './watchdog-core.js'

/**
 * Plugin-operation accounting and enforcement: hot-operation counting, the
 * per-target quarantine registry, rollback of additive operations at the
 * pause/frost waterlines, manual release, and the pending-boot plan written for
 * an external launcher.
 *
 * The module only talks to the plugin manager through `refs.manager`; it never
 * writes plugin files.
 */
/** A single >=3000 MB plugin may not close the whole third-party set in one pass. */
export const MAX_THIRD_PARTY_CLOSURES = 5

export function createEnforcement({ watchdogState, refs, dataDir, journalDir, exemptions, appendIncident, persist }) {
  const inflight = new Map()
  const operationWindows = []
  let operationWindowUntil = 0
  let lastOperationAt = 0

  function writePendingBoot(payload) {
    try {
      const file = path.join(dataDir, 'pending-boot.json')
      const rank = { L0: 0, L1: 1, L2: 2, L3: 3 }
      let previous = null
      try {
        previous = JSON.parse(fs.readFileSync(file, 'utf8'))
      } catch {
        previous = null
      }
      if (previous && (rank[previous.level] || 0) > (rank[payload.level] || 0)) return
      fs.writeFileSync(file, JSON.stringify({ writtenAt: new Date().toISOString(), ...payload }, null, 2) + '\n')
    } catch {
      // an external launcher reads this file; best effort
    }
  }

  async function disableTarget(target, reason) {
    const manager = refs.manager
    if (!manager || typeof manager.setBundleEnabled !== 'function') {
      watchdogState.degraded = `disable ${target}: pluginManager unavailable`
      return false
    }
    watchdogState.suppressUntil.set(target, Date.now() + 8000)
    for (let attempt = 0; attempt < 2; attempt += 1) {
      try {
        const rows = typeof manager.listBundles === 'function' ? await manager.listBundles() : []
        const row = rows.find((entry) => entry.name === target)
        if (row && row.enabled === false) return true
        await manager.setBundleEnabled(target, false)
        appendIncident({ kind: 'close', target, reason, detail: `已自动关闭 ${target}（${reason}）` })
        return true
      } catch (error) {
        if (attempt === 0) await new Promise((resolve) => setTimeout(resolve, 500))
        else {
          appendIncident({ kind: 'close-failed', target, reason, detail: `自动关闭 ${target} 失败：${String((error && error.message) || error)}` })
          watchdogState.degraded = `disable ${target}: ${String((error && error.message) || error)}`
          return false
        }
      }
    }
    return false
  }

  function releaseTarget(target) {
    const entry = watchdogState.quarantine.get(target)
    if (!entry) return { ok: false, error: 'not quarantined' }
    watchdogState.quarantine.delete(target)
    watchdogState.suppressUntil.set(target, Date.now() + 5000)
    appendIncident({ kind: 'release', target, detail: `用户假释 ${target}（原因 ${entry.reason}）` })
    persist(true)
    return { ok: true }
  }

  async function disableMany(targets, reason) {
    const unique = [...new Set(targets)].filter((target) => target && !exemptions.has(target) && !watchdogState.quarantine.has(target))
    const disabled = []
    for (const target of unique) {
      watchdogState.quarantine.set(target, { reason, level: 'crash', at: new Date().toISOString(), release: 'manual', evidence: reason, remediation: remediationFor(reason) })
      if (await disableTarget(target, reason)) disabled.push(target)
      await new Promise((resolve) => setTimeout(resolve, 300))
    }
    return disabled
  }

  async function reapplyQuarantine() {
    for (const [target, entry] of watchdogState.quarantine) {
      if (entry.release === 'restart') {
        watchdogState.quarantine.delete(target)
        continue
      }
      void disableTarget(target, 'boot-reapply')
    }
  }

  function onOperation(operation) {
    try {
      if (!operation || typeof operation !== 'object') return
      const target = typeof operation.target === 'string' && operation.target ? operation.target : null
      if (!target) return
      if ((watchdogState.suppressUntil.get(target) || 0) > Date.now()) return
      const entry = watchdogState.operations.get(target) || { count: 0, estimateMB: 0, lastAt: null, version: operation.version || null, lastApplication: null }
      operationWindowUntil = Date.now() + 4000
      lastOperationAt = Date.now()
      if (operation.phase === 'started') {
        operationWindows.push({ startedAt: Date.now(), settledAt: null })
        if (operationWindows.length > 20) operationWindows.shift()
        inflight.set(operation.id, { target, heapBefore: watchdogState.latest ? watchdogState.latest.heapUsed : 0 })
        entry.lastAt = operation.time || entry.lastAt
        watchdogState.operations.set(target, entry)
        return
      }
      if (operation.phase !== 'settled') return
      for (let i = operationWindows.length - 1; i >= 0; i -= 1) {
        if (operationWindows[i].settledAt === null) {
          operationWindows[i].settledAt = Date.now()
          break
        }
      }
      const weight = operationWeight(operation)
      if (weight > 0 && operation.application !== 'failed' && operation.application !== 'cancelled') {
        entry.count = Math.round((Number(entry.count) + weight) * 10) / 10
      }
      const before = inflight.get(operation.id)
      inflight.delete(operation.id)
      if (before && watchdogState.latest && before.heapBefore > 0) {
        const deltaMB = Math.max(0, Math.round((watchdogState.latest.heapUsed - before.heapBefore) / (1024 * 1024)))
        entry.estimateMB = mergeEstimate(entry.estimateMB, deltaMB)
      }
      entry.lastAt = operation.time || entry.lastAt
      entry.version = operation.version || entry.version
      entry.lastApplication = operation.application || null
      watchdogState.operations.set(target, entry)
      const additive = operation.stage === 'install' || (operation.stage === 'enable' && operation.enabled !== false)
      if (operation.stage === 'enable' && operation.enabled !== false && watchdogState.enforce && watchdogState.quarantine.has(target)) {
        const quarantined = watchdogState.quarantine.get(target)
        appendIncident({
          kind: 'quarantine-reminder',
          target,
          level: quarantined.level,
          notify: true,
          detail: '请到，AI监狱释放该插件！再开启。（原因：' + quarantined.reason + '；本次开启已被立即关闭）',
        })
        void disableTarget(target, 'quarantined-enable')
      }
      if (
        watchdogState.enforce
        && additive
        && target
        && !exemptions.has(target)
        && (watchdogState.waterline === 'frost' || watchdogState.waterline === 'pause')
        && !(Number.isFinite(watchdogState.graceUntil) && Date.now() < watchdogState.graceUntil)
      ) {
        const level = watchdogState.waterline
        appendIncident({
          kind: level === 'frost' ? 'freeze-rollback' : 'pause-rollback',
          target,
          level,
          detail: (level === 'frost' ? '冻结' : '暂停') + '期间检测到面板/网页发起的增内存操作，已立即回滚关闭 ' + target,
        })
        void disableTarget(target, level + '-rollback')
      }
      if (operation.reason === 'remove' && operation.application === 'applied') {
        watchdogState.operations.delete(target)
        return
      }
      if (watchdogState.enforce && !exemptions.has(target) && !watchdogState.quarantine.has(target) && hotOperationTriggered(entry.count)) {
        watchdogState.quarantine.set(target, {
          reason: 'hot-ops',
          level: 'L2',
          at: new Date().toISOString(),
          release: 'restart',
          evidence: `本次运行内热操作计数 ${entry.count} > 5（安装/启用/热切换）`,
          remediation: remediationFor('hot-ops', { count: entry.count }),
        })
        appendIncident({ kind: 'quarantine', target, level: 'L2', reason: 'hot-ops', detail: `热操作 ${entry.count} 次 > 5，自动关闭` })
        persist(true)
        void disableTarget(target, 'hot-ops')
      }
      persist(true)
    } catch (error) {
      watchdogState.degraded = `operation handler: ${String((error && error.message) || error)}`
    }
  }

  const TIER_RANK = { ok: 0, warn: 1, throttle: 2, close: 3, uninstall: 4, 'all-third-party': 5 }
  let tierRunPromise = null

  /** Best available single-plugin memory estimate: measured module load + child RSS, else operation estimate. */
  function measuredMemoryMB(target) {
    const budget = watchdogState.budgets && watchdogState.budgets[target]
    const moduleMB = budget && Number.isFinite(Number(budget.moduleLoadMB)) ? Number(budget.moduleLoadMB) : 0
    const rssMB = watchdogState.rss ? (Number(watchdogState.rss[target]) || 0) / (1024 * 1024) : 0
    const opMB = (watchdogState.operations.get(target) || {}).estimateMB || 0
    if (moduleMB > 0 || rssMB > 0) return Math.round(moduleMB + rssMB)
    return Math.round(opMB)
  }

  /** True only when the occupancy came from a real measurement, not an operation estimate. */
  function hasMeasuredMemory(target) {
    const budget = watchdogState.budgets && watchdogState.budgets[target]
    const moduleMB = budget && Number.isFinite(Number(budget.moduleLoadMB)) ? Number(budget.moduleLoadMB) : 0
    const rssMB = watchdogState.rss ? (Number(watchdogState.rss[target]) || 0) / (1024 * 1024) : 0
    return moduleMB > 0 || rssMB > 0
  }
  /**
   * Plugin-prison memory tiers (single-plugin occupancy):
   *   300 MB warn · 500 MB throttle · 1000 MB force close ·
   *   2000 MB force uninstall · 3000 MB close all third-party plugins.
   * Enforce mode acts; observe mode only records the tier changes.
   */
  async function enforceMemoryTiers() {
    if (tierRunPromise) return tierRunPromise
    tierRunPromise = doEnforceMemoryTiers().finally(() => { tierRunPromise = null })
    return tierRunPromise
  }

  async function doEnforceMemoryTiers() {
    const manager = refs.manager
    if (!manager || typeof manager.listBundles !== 'function') return null
    let rows
    try {
      rows = await manager.listBundles()
    } catch {
      return null
    }
    const enabled = rows.filter((row) => row.enabled && !exemptions.has(row.name))
    const summary = { checked: enabled.length, warned: [], throttled: [], closed: [], uninstalled: [], thirdParty: [], thirdPartyPending: [] }
    watchdogState.thirdPartyPending = []
    for (const row of enabled) {
      const mb = measuredMemoryMB(row.name)
      const tier = memoryTierFor(mb)
      const previous = watchdogState.memoryTiers.get(row.name)
      const previousRank = TIER_RANK[previous ? previous.tier : 'ok'] || 0
      const rank = TIER_RANK[tier] || 0
      if (tier === 'ok') {
        if (previous) watchdogState.memoryTiers.delete(row.name)
        continue
      }
      watchdogState.memoryTiers.set(row.name, { tier, mb, at: new Date().toISOString(), version: row.version || null })
      if (!watchdogState.enforce) {
        if (rank > previousRank) appendIncident({ kind: 'plugin-memory', level: tier, target: row.name, mb, detail: '插件内存 ' + mb + ' MB 达到 ' + tier + ' 档（observe：仅记录）' })
        continue
      }
      if (rank <= previousRank) continue
      if (tier === 'warn') {
        summary.warned.push(row.name)
        appendIncident({ kind: 'plugin-memory', level: 'warn', target: row.name, mb, detail: '插件内存 ' + mb + ' MB ≥ 300 MB：警告' })
        continue
      }
      if (tier === 'throttle') {
        summary.throttled.push(row.name)
        appendIncident({ kind: 'plugin-memory', level: 'throttle', target: row.name, mb, detail: '插件内存 ' + mb + ' MB ≥ 500 MB：限制该插件的增内存操作（增长目标 ≤1 MB/s）' })
        continue
      }
      // close (>=1000) / uninstall (>=2000) / all-third-party (>=3000)
      watchdogState.quarantine.set(row.name, {
        reason: 'memory',
        level: 'L2',
        at: new Date().toISOString(),
        release: 'manual',
        evidence: '实测/估算 ' + mb + ' MB',
        remediation: remediationFor('memory'),
      })
      if (tier === 'uninstall' && !hasMeasuredMemory(row.name)) {
        await disableTarget(row.name, 'memory-uninstall-unverified')
        summary.closed.push(row.name)
        appendIncident({ kind: 'plugin-memory', level: 'close', target: row.name, mb, detail: '插件内存 ' + mb + ' MB 仅来自操作估算（无实测）；为避免误卸载，改为强制关闭' })
        writePendingBoot({ level: 'L1', reason: 'memory', disable: [row.name], keep: [] })
        continue
      }
      if (tier === 'uninstall') {
        let removed = false
        if (typeof manager.removeBundle === 'function') {
          try {
            const result = await manager.removeBundle(row.name)
            removed = Boolean(result && result.application !== 'failed')
          } catch (error) {
            appendIncident({ kind: 'plugin-memory', level: 'uninstall', target: row.name, mb, detail: '强制卸载 ' + row.name + ' 失败：' + String((error && error.message) || error) + '；改为强制关闭' })
          }
        }
        if (!removed) await disableTarget(row.name, 'memory-over-2000MB')
        summary.uninstalled.push(row.name)
        appendIncident({ kind: 'plugin-memory', level: 'uninstall', target: row.name, mb, detail: removed ? '插件内存 ' + mb + ' MB ≥ 2000 MB：已强制卸载' : '插件内存 ' + mb + ' MB ≥ 2000 MB：卸载失败，已强制关闭' })
        writePendingBoot({ level: 'L1', reason: 'memory', disable: [row.name], keep: [] })
        continue
      }
      await disableTarget(row.name, 'memory-over-' + tier)
      summary.closed.push(row.name)
      appendIncident({ kind: 'plugin-memory', level: tier, target: row.name, mb, detail: '插件内存 ' + mb + ' MB ≥ ' + (tier === 'all-third-party' ? 3000 : 1000) + ' MB：强制关闭' })
      if (tier === 'all-third-party') {
        const others = enabled
          .filter((other) => other.name !== row.name && other.official !== true && !String(other.name).startsWith('@deepseek-ai/'))
          .filter((other) => !watchdogState.quarantine.has(other.name))
          .map((other) => ({ name: other.name, mb: measuredMemoryMB(other.name) }))
          .sort((a, b) => b.mb - a.mb)
        const selected = others.slice(0, Math.max(0, MAX_THIRD_PARTY_CLOSURES - summary.thirdParty.length))
        const pending = others.slice(Math.max(0, MAX_THIRD_PARTY_CLOSURES - summary.thirdParty.length))
        for (const other of selected) {
          watchdogState.quarantine.set(other.name, { reason: 'memory', level: 'L2', at: new Date().toISOString(), release: 'manual', evidence: '同批第三方关闭（' + row.name + ' ≥3000 MB）', remediation: remediationFor('memory') })
          await disableTarget(other.name, 'memory-all-third-party')
          summary.thirdParty.push(other.name)
        }
        watchdogState.thirdPartyPending = [...(watchdogState.thirdPartyPending || []), ...pending.map((other) => ({ name: other.name, mb: other.mb, reason: 'all-third-party-cap' }))]
        summary.thirdPartyPending = watchdogState.thirdPartyPending.map((other) => other.name)
        appendIncident({ kind: 'plugin-memory', level: 'all-third-party', target: row.name, mb, detail: '单个插件 ≥3000 MB：本轮关闭 ' + summary.thirdParty.length + ' 个第三方插件（上限 ' + MAX_THIRD_PARTY_CLOSURES + '）；待人工处理 ' + summary.thirdPartyPending.length + ' 个' })
      }
      writePendingBoot({ level: 'L1', reason: 'memory', disable: [row.name, ...summary.thirdParty], keep: [] })
    }
    if (summary.closed.length || summary.uninstalled.length || summary.thirdParty.length || summary.thirdPartyPending.length) persist(true)
    return summary
  }

  /** Profile drift detected by the tamper monitor: record, attribute, optionally enforce. */
  function handleTamper(result) {
    if (!result || !Array.isArray(result.diffs) || result.diffs.length === 0) return
    const files = result.diffs.map((diff) => diff.file)
    const policy = (watchdogState.tamper && watchdogState.tamper.policy) || 'observe'
    appendIncident({
      kind: 'tamper',
      files,
      policy,
      restored: result.restored,
      notify: policy === 'alert',
      detail:
        '配置漂移：' + files.join(', ')
        + (policy === 'observe' ? '（observe：仅记录，不恢复不关闭）' : '')
        + (policy === 'alert' ? '（alert：发出告警事件，不自动恢复；是否隔离由人工决定）' : ''),
    })
    let target = null
    let evidence = []
    try {
      const records = readJournalRecords(journalDir).filter((record) => {
        const at = Date.parse((record && record.operation && record.operation.time) || 0)
        return Number.isFinite(at) && Date.now() - at < 6 * 3600 * 1000
      })
      const attribution = attributeCrash(records, { hostRunId: null })
      target = attribution.picked ? attribution.picked.target : null
      evidence = attribution.candidates
    } catch {
      target = null
    }
    const applied = Boolean(watchdogState.enforce && target && policy === 'restore')
    const plan = { at: new Date().toISOString(), level: 'L1', crash: null, reason: 'tamper', remediation: remediationFor('tamper'), target, inFlight: false, evidence: { files: result.diffs, candidates: evidence }, applied }
    watchdogState.plans.push(plan)
    if (watchdogState.plans.length > 20) watchdogState.plans = watchdogState.plans.slice(-20)
    if (applied) {
      watchdogState.quarantine.set(target, { reason: 'tamper', level: 'L1', at: plan.at, release: 'manual', evidence: '配置漂移：' + files.join(', '), remediation: remediationFor('tamper') })
      void disableTarget(target, 'tamper')
      writePendingBoot({ level: 'L1', reason: 'tamper', disable: [target], keep: [] })
      appendIncident({ kind: 'tamper-enforced', target, files, detail: '配置漂移后已隔离 ' + target })
    }
    persist(true)
  }

  /** True while the plugin manager itself is expected to be writing profile files. */
  function shouldIgnoreWrites() {
    return Date.now() < operationWindowUntil || Date.now() - lastOperationAt < 120000
  }

  /** True when a file mtime falls inside a recorded plugin-manager operation window. */
  function isWithinOperationWindow(mtimeMs) {
    const now = Date.now()
    return operationWindows.some((window) => mtimeMs >= window.startedAt - 5000 && mtimeMs <= (window.settledAt || now) + 5000)
  }

  return { onOperation, disableTarget, releaseTarget, disableMany, writePendingBoot, reapplyQuarantine, handleTamper, shouldIgnoreWrites, isWithinOperationWindow, enforceMemoryTiers }
}
