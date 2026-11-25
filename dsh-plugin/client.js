// Client half of @twinsearth/ai-prison-dsh-plugin: a sidebar entry plus the
// panel that renders the L0–L5 matrix and the durable ledger's audit-chain
// state. Data comes from GET /api/ai-prison/status (read-only).
window.__ModuleLoader__.load({
  id: '@twinsearth/ai-prison-dsh-plugin',
  factory: (require) => {
    const React = require('react')
    const h = React.createElement

    const PANEL_ID = 'ai-prison'
    const NS = 'ai-prison'
    const STATUS_URL = 'api/ai-prison/status'
    const RUN_URL = 'api/ai-prison/run'
    const WATCHDOG_URL = 'api/ai-prison/watchdog/status'
    const WATCHDOG_ACTION_URL = 'api/ai-prison/watchdog/action'

    const DICTS = {
      en: {
        'panel.label': 'AI Prison',
        'panel.version': 'AI Prison v{version}',
        'page.title': 'AI Prison',
        'page.subtitle': 'Graduated capability deprivation for agents. Every backend is simulated.',
        'matrix.title': 'L0–L5 grading matrix',
        'matrix.code': 'Level',
        'matrix.name': 'Name',
        'matrix.duration': 'Duration',
        'matrix.deprivations': 'Deprived capabilities',
        'matrix.empty': 'The CLI is not ready, so the matrix cannot be read.',
        'ledger.title': 'Audit chain',
        'ledger.dir': 'Ledger directory',
        'ledger.records': 'Records',
        'ledger.segments': 'Segments',
        'ledger.integrity': 'Integrity',
        'ledger.files': 'Files',
        'ledger.detail': 'Detail',
        'ledger.empty': 'No ledger yet: run the demo or the drill to create one.',
        'integrity.ok': 'INTEGRITY OK',
        'integrity.failed': 'Check failed',
        'integrity.none': 'Not initialized',
        'cli.found': 'aiprison {version}',
        'cli.missing': 'CLI not found',
        'cli.source': 'Source',
        'action.refresh': 'Refresh',
        'action.loading': 'Reading…',
        'action.runDemo': 'Run demo',
        'action.runDrill': 'Run drill',
        'action.confirmRun': 'Click again: move {records} segments to .trash and run',
        'action.running': 'Running…',
        'ledger.actions': 'Explicit actions',
        'ledger.actionsHint': 'These buttons run the bundled CLI. Existing segments are moved to .trash first, so every run starts from a consistent baseline.',
        'ledger.resetHint': 'The ledger already exists: a run moves its segments to .trash first, then executes. Click twice to confirm.',
        'ledger.lastAction': 'Last action',
        'meta.updated': 'Updated {time}',
        'meta.readonly': 'Read-only view: the panel never writes by itself; only the buttons above do.',
        'watchdog.title': 'Memory watchdog',
        'watchdog.mode': 'Observe mode: records only, never closes plugins',
        'watchdog.modeEnforce': 'Enforce mode: over-limit operations are rolled back, closed or quarantined',
        'watchdog.enforceTitle': 'Enforcement',
        'watchdog.actionError': 'Panel action failed',
        'watchdog.enforceOn': 'enforce',
        'watchdog.observeOnly': 'observe only',
        'watchdog.freezeHint': 'Heap is frozen at {level}: plugin operations are blocked except release actions. Close the biggest plugins below or restart DSH.',
        'watchdog.confirmPause': 'I understand: allow plugin operations for 10 minutes',
        'watchdog.quarantine': 'Plugin prison',
        'watchdog.noQuarantine': 'No quarantined plugins.',
        'watchdog.thirdPartyPending': 'Third-party plugins awaiting manual review (cap hit): {n}',
        'watchdog.release': 'Release',
        'watchdog.topFive': 'Top {n} by estimated heap',
        'watchdog.close': 'Arrest now',
        'watchdog.plans': 'Crash plans',
        'watchdog.baseline': 'Clean baseline: {at}',
        'watchdog.tamper': 'Profile watch: {n} critical file(s) · policy {policy} · last check {at}',
        'watchdog.measure': 'Measure plugin budgets now',
        'watchdog.measureBlocked': 'Heap is frozen: measuring spawns child processes, so it stays disabled until the heap recovers.',
        'watchdog.heap6gOff': 'Request 6 GB heap on next launch',
        'watchdog.heap6gOn': 'Cancel the 6 GB heap request',
        'watchdog.heap6gPending': 'Written to the watchdog state directory; an external launcher must consume it (none is installed right now).',
        'watchdog.pendingBoot': 'pending-boot plan ({level}) was written, but no external consumer is installed: only this run\'s in-process isolation applies. Handle the next start manually.',
        'watchdog.pendingBootConsumer': 'consumer: {consumer}',
        'watchdog.disabled': 'Watchdog row not enabled; heap observation is unavailable.',
        'watchdog.heap': 'Heap used',
        'watchdog.limit': 'Heap limit',
        'watchdog.rss': 'RSS',
        'watchdog.waterline': 'Waterline',
        'watchdog.thresholds': 'Warn / pause / block',
        'waterline.ok': 'OK',
        'waterline.easy': 'Relaxed',
        'waterline.normal': 'Normal',
        'waterline.danger': 'Danger',
        'waterline.warn': 'Warning',
        'waterline.pause': 'Paused',
        'waterline.frost': 'Frozen',
        'waterline.block': 'Blocked',
        'prison.title': 'Plugin prison',
        'prison.top': 'Top {n} plugins by estimated heap',
        'prison.ops': 'Hot ops',
        'prison.estimate': 'Est.',
        'prison.plugin': 'Plugin',
        'prison.rss': 'Child RSS',
        'watchdog.memoryTier': 'Memory tier',
        'crashPlan.title': 'Crash degradation plan',
        'crashPlan.level': 'Level {level}',
        'crashPlan.window': '{n} crash(es) in {minutes} min',
        'crashPlan.next': 'next {level}',
        'crashPlan.max': 'at the highest level',
        'crashPlan.step.isolate-target': 'L1 isolate the attributed or in-flight plugin',
        'crashPlan.step.revert-changed': 'L2 also close plugins changed since the clean baseline',
        'crashPlan.step.safe-mode': 'L3 safe mode: keep the clean baseline, close newer plugins',
        'crashPlan.last': 'last plan {at} · {target} · {applied}',
        'crashPlan.applied': 'applied',
        'crashPlan.notApplied': 'not applied',
        'memoryTier.ok': 'Normal',
        'memoryTier.warn': 'Warning',
        'memoryTier.throttle': 'Throttled',
        'memoryTier.close': 'Closed',
        'memoryTier.uninstall': 'Uninstalled',
        'memoryTier.all-third-party': 'All third-party',
        'prison.incidents': 'Recent incidents',
        'prison.empty': 'No quarantined plugins.',
        'prison.noestimates': 'No plugin operations observed yet.',
        'error.title': 'Cannot read the status',
        'error.hint': 'Check that the bundle is enabled and the aiprison CLI resolves for this platform.',
      },
      zh: {
        'panel.label': 'AI Prison',
        'panel.version': 'AI Prison v{version}',
        'page.title': 'AI Prison · AI 监狱',
        'page.subtitle': '面向 AI Agent 的分级能力剥夺与行为矫正；所有后端均为内存模拟。',
        'matrix.title': 'L0–L5 分级矩阵',
        'matrix.code': '级别',
        'matrix.name': '名称',
        'matrix.duration': '期限',
        'matrix.deprivations': '被剥夺的能力',
        'matrix.empty': 'CLI 未就绪，暂时无法读取矩阵。',
        'ledger.title': '审计链状态',
        'ledger.dir': '数据目录',
        'ledger.records': '记录数',
        'ledger.segments': '段数',
        'ledger.integrity': '完整性',
        'ledger.files': '文件数',
        'ledger.detail': '详情',
        'ledger.empty': '账本尚未初始化：运行 demo 或 drill 即可创建。',
        'integrity.ok': 'INTEGRITY OK',
        'integrity.failed': '校验失败',
        'integrity.none': '未初始化',
        'cli.found': 'aiprison {version}',
        'cli.missing': 'CLI 未找到',
        'cli.source': '来源',
        'action.refresh': '刷新',
        'action.loading': '读取中…',
        'action.runDemo': '运行 demo',
        'action.runDrill': '运行演练',
        'action.confirmRun': '再次点击：将 {records} 条记录移入 .trash 并运行',
        'action.running': '运行中…',
        'ledger.actions': '显式操作',
        'ledger.actionsHint': '这些按钮运行内置 CLI；账本已存在时会先把账本段移入 .trash，保证每次从一致基线开始。',
        'ledger.resetHint': '账本已存在：运行会先移入 .trash 再执行；再次点击确认。',
        'ledger.lastAction': '最近操作',
        'meta.updated': '更新于 {time}',
        'meta.readonly': '只读视图：面板不会自行写入账本，只有上方按钮会写入。',
        'watchdog.title': '内存看护',
        'watchdog.mode': '观察模式：只记录，不关闭插件',
        'watchdog.modeEnforce': '执行模式：超限操作会被回滚、关闭或隔离',
        'watchdog.enforceTitle': '执行状态',
        'watchdog.actionError': '面板操作失败',
        'watchdog.enforceOn': '执行中',
        'watchdog.observeOnly': '仅观察',
        'watchdog.freezeHint': '堆水位已冻结（{level}）：除释放类操作外，所有插件操作被禁止。请关闭下方内存最大的插件或重启 DSH。',
        'watchdog.confirmPause': '我已知晓：允许插件操作 10 分钟',
        'watchdog.quarantine': '插件监狱',
        'watchdog.noQuarantine': '没有被隔离的插件。',
        'watchdog.thirdPartyPending': '待人工处理的第三方插件（触发上限）：{n}',
        'watchdog.release': '假释',
        'watchdog.topFive': '按估算内存排序的前 {n} 个插件',
        'watchdog.close': '立即缉拿',
        'watchdog.plans': '崩溃降级计划',
        'watchdog.baseline': '干净基线：{at}',
        'watchdog.tamper': '配置监控：{n} 个关键文件 · 策略 {policy} · 最近检查 {at}',
        'watchdog.measure': '立即测量插件预算',
        'watchdog.measureBlocked': '堆已冻结：测量会启动子进程，恢复前暂停测量。',
        'watchdog.heap6gOff': '请求下次启动使用 6GB 堆',
        'watchdog.heap6gOn': '取消 6GB 堆请求',
        'watchdog.heap6gPending': '已写入 watchdog 状态目录；需要外部启动器消费（当前未安装）。',
        'watchdog.pendingBoot': '已写入 pending-boot 计划（{level}），但当前未安装外部消费器：只有本次运行内的进程内隔离生效，下次启动需人工处理。',
        'watchdog.pendingBootConsumer': '消费器：{consumer}',
        'watchdog.disabled': 'watchdog 行未启用，暂时无法观察内存。',
        'watchdog.heap': '已用堆',
        'watchdog.limit': '堆上限',
        'watchdog.rss': 'RSS',
        'watchdog.waterline': '水位',
        'watchdog.thresholds': '提醒 / 暂停 / 禁止',
        'waterline.ok': '正常',
        'waterline.easy': '轻松',
        'waterline.normal': '正常',
        'waterline.danger': '危险',
        'waterline.warn': '警告',
        'waterline.pause': '暂停',
        'waterline.frost': '已冻结',
        'waterline.block': '禁止',
        'prison.title': '插件监狱',
        'prison.top': '按估算内存排序的前 {n} 个插件',
        'prison.ops': '热操作',
        'prison.estimate': '估算',
        'prison.plugin': '插件',
        'prison.rss': '子进程 RSS',
        'watchdog.memoryTier': '内存档位',
        'crashPlan.title': '崩溃降级计划',
        'crashPlan.level': '当前 {level}',
        'crashPlan.window': '{minutes} 分钟窗口内 {n} 次崩溃',
        'crashPlan.next': '下一档 {level}',
        'crashPlan.max': '已到最高档',
        'crashPlan.step.isolate-target': 'L1 隔离崩溃归因（或崩溃时在途）的插件',
        'crashPlan.step.revert-changed': 'L2 同时关闭干净基线之后变更的插件',
        'crashPlan.step.safe-mode': 'L3 安全模式：保留干净基线，关闭所有新增插件',
        'crashPlan.last': '最近计划 {at} · {target} · {applied}',
        'crashPlan.applied': '已执行',
        'crashPlan.notApplied': '未执行',
        'memoryTier.ok': '正常',
        'memoryTier.warn': '警告',
        'memoryTier.throttle': '节流',
        'memoryTier.close': '关闭',
        'memoryTier.uninstall': '卸载',
        'memoryTier.all-third-party': '全部第三方',
        'prison.incidents': '最近事件',
        'prison.empty': '没有被隔离的插件。',
        'prison.noestimates': '还没有观察到插件操作。',
        'error.title': '无法读取状态',
        'error.hint': '请确认插件已启用，且本平台能解析到 aiprison CLI。',
      },
    }

    const fallbackDict = String(navigator.language || '').toLowerCase().startsWith('zh') ? DICTS.zh : DICTS.en
    let translate

    function t(key, vars) {
      let text = (typeof translate === 'function' ? translate(key) : undefined) || fallbackDict[key] || key
      if (vars) {
        for (const [name, value] of Object.entries(vars)) text = text.split(`{${name}}`).join(String(value))
      }
      return text
    }

    const C = {
      page: {
        maxWidth: 980,
        margin: '0 auto',
        padding: '20px 24px 16px',
        color: 'var(--dsw-alias-label-primary, #0f1115)',
        boxSizing: 'border-box',
        height: '100%',
        maxHeight: '100%',
        minHeight: 0,
        overflowY: 'auto',
        overflowX: 'hidden',
        overscrollBehavior: 'contain',
        scrollbarGutter: 'stable',
        scrollbarWidth: 'thin',
        scrollbarColor: 'rgba(128,128,128,.55) transparent',
        WebkitOverflowScrolling: 'touch',
      },
      header: { display: 'flex', alignItems: 'center', gap: 12, marginBottom: 14 },
      title: { fontSize: 18, fontWeight: 600, margin: 0 },
      sub: { fontSize: 12, color: 'var(--dsw-alias-label-secondary, #61666b)', margin: '3px 0 0' },
      pill: {
        marginLeft: 'auto',
        display: 'inline-flex',
        alignItems: 'center',
        gap: 6,
        fontSize: 12,
        fontWeight: 500,
        padding: '4px 10px',
        borderRadius: 999,
        border: '1px solid var(--dsw-alias-border-l2, rgba(0,0,0,.12))',
        whiteSpace: 'nowrap',
      },
      dot: { width: 8, height: 8, borderRadius: 999, display: 'inline-block' },
      card: {
        border: '1px solid var(--dsw-alias-border-l2, rgba(0,0,0,.12))',
        borderRadius: 12,
        padding: 12,
        marginBottom: 12,
        background: 'var(--dsw-alias-bg-layer-1, transparent)',
      },
      cardHead: { display: 'flex', alignItems: 'center', gap: 8, marginBottom: 8, fontSize: 13, fontWeight: 600 },
      table: { width: '100%', borderCollapse: 'collapse', fontSize: 12.5 },
      th: {
        textAlign: 'left',
        fontWeight: 500,
        color: 'var(--dsw-alias-label-secondary, #61666b)',
        padding: '6px 8px',
        borderBottom: '1px solid var(--dsw-alias-border-l2, rgba(0,0,0,.12))',
        whiteSpace: 'nowrap',
      },
      td: {
        padding: '7px 8px',
        borderBottom: '1px solid var(--dsw-alias-border-l1, rgba(0,0,0,.06))',
        verticalAlign: 'top',
      },
      mono: { fontFamily: 'ui-monospace, Consolas, monospace', fontSize: 12 },
      code: { fontWeight: 600, fontFamily: 'ui-monospace, Consolas, monospace' },
      chips: { display: 'flex', flexWrap: 'wrap', gap: 4 },
      chip: {
        fontSize: 11.5,
        padding: '1px 7px',
        borderRadius: 999,
        border: '1px solid var(--dsw-alias-border-l2, rgba(0,0,0,.12))',
        color: 'var(--dsw-alias-label-secondary, #61666b)',
        whiteSpace: 'nowrap',
      },
      grid: { display: 'grid', gridTemplateColumns: 'minmax(110px, max-content) minmax(0, 1fr)', gap: '6px 14px', fontSize: 12.5 },
      key: { color: 'var(--dsw-alias-label-secondary, #61666b)' },
      muted: { color: 'var(--dsw-alias-label-secondary, #61666b)' },
      btn: {
        padding: '5px 10px',
        borderRadius: 7,
        border: '1px solid var(--dsw-alias-border-l2, rgba(0,0,0,.16))',
        background: 'transparent',
        color: 'inherit',
        cursor: 'pointer',
        fontSize: 12,
        marginLeft: 8,
      },
      error: { color: '#c81e1e' },
      ok: { color: '#15803d' },
      actionBtn: {
        padding: '5px 12px',
        borderRadius: 7,
        border: '1px solid var(--dsw-alias-border-l2, rgba(0,0,0,.16))',
        background: 'transparent',
        color: 'inherit',
        cursor: 'pointer',
        fontSize: 12,
      },
    }

    function PrisonIcon({ size = 36, active = true }) {
      return h(
        'svg',
        {
          width: size,
          height: size,
          viewBox: '0 0 36 36',
          role: 'img',
          'aria-hidden': true,
          style: { display: 'block', opacity: active ? 1 : 0.75, flex: 'none' },
        },
        h('path', {
          d: 'M18 4.4 29.4 9v9.1c0 6.7-11.4 13.2-11.4 13.2S6.6 24.7 6.6 18.1V9z',
          fill: 'none',
          stroke: '#334155',
          strokeWidth: 2.2,
          strokeLinejoin: 'round',
        }),
        h('path', { d: 'M14 12.6v11.2M18 11.4v13.4M22 12.6v11.2', stroke: '#334155', strokeWidth: 2.2, strokeLinecap: 'round' }),
      )
    }

    const MEMORY_TIER_KEYS = { ok: 'memoryTier.ok', warn: 'memoryTier.warn', throttle: 'memoryTier.throttle', close: 'memoryTier.close', uninstall: 'memoryTier.uninstall', 'all-third-party': 'memoryTier.all-third-party' }
    const CRASH_STEP_KEYS = { 'isolate-target': 'crashPlan.step.isolate-target', 'revert-changed': 'crashPlan.step.revert-changed', 'safe-mode': 'crashPlan.step.safe-mode' }

    function Pill({ tone, text }) {
      const palette = { ok: C.ok.color, info: '#2563eb', warn: '#b45309', error: C.error.color }
      const color = palette[tone] || C.muted.color
      return h('span', { style: C.pill }, h('span', { style: { ...C.dot, background: color } }), h('span', null, text))
    }

    function fetchStatus(signal) {
      return fetch(new URL(STATUS_URL, document.baseURI), {
        credentials: 'same-origin',
        headers: { accept: 'application/json' },
        signal,
      }).then((response) => {
        if (!response.ok) throw new Error(`HTTP ${response.status}`)
        return response.json()
      })
    }

    /** Run one explicit ledger action through the Host half. */
    function postRun(action) {
      return fetch(new URL(RUN_URL, document.baseURI), {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'content-type': 'application/json', accept: 'application/json' },
        body: JSON.stringify({ action }),
      }).then(async (response) => {
        const payload = await response.json().catch(() => ({}))
        if (!response.ok || payload.ok !== true) {
          throw new Error(payload.error || payload.summary || `HTTP ${response.status}`)
        }
        return payload
      })
    }

    /** Read-only watchdog snapshot; a missing row (or 404) simply hides the cards. */
    function fetchWatchdog(signal) {
      return fetch(new URL(WATCHDOG_URL, document.baseURI), {
        credentials: 'same-origin',
        headers: { accept: 'application/json' },
        signal,
      }).then((response) => {
        if (!response.ok) return null
        return response.json().then((payload) => (payload && payload.ok ? payload : null))
      })
    }


    function postWatchdogAction(action, target, extra = {}) {
      return fetch(new URL(WATCHDOG_ACTION_URL, document.baseURI), {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'content-type': 'application/json', accept: 'application/json' },
        body: JSON.stringify({ action, target, ...extra }),
      }).then(async (response) => {
        const payload = await response.json().catch(() => ({}))
        if (!response.ok || payload.ok !== true) {
          throw new Error(payload.error || `HTTP ${response.status}`)
        }
        return payload
      })
    }

    function MemoryCard({ watchdog }) {
      if (!watchdog) {
        return h(
          'section',
          { style: C.card },
          h('div', { style: C.cardHead }, t('watchdog.title')),
          h('p', { style: { ...C.muted, fontSize: 12, margin: 0 } }, t('watchdog.disabled')),
        )
      }
      const heap = watchdog.heap || {}
      const thresholds = watchdog.thresholds || {}
      const display = thresholds.display || thresholds
      const ratio = heap.heapLimit > 0 ? Math.min(1, heap.heapUsed / heap.heapLimit) : 0
      // 水位只读看护器的真源：不再用 heapUsed 百分比另算一套等级。
      const level = watchdog.waterline || 'ok'
      const band =
        level === 'frost'
          ? { tone: 'error', key: 'waterline.frost', color: C.error.color }
          : level === 'pause'
            ? { tone: 'warn', key: 'waterline.pause', color: '#b45309' }
            : level === 'warn'
              ? { tone: 'warn', key: 'waterline.warn', color: '#b45309' }
              : { tone: 'ok', key: 'waterline.ok', color: C.ok.color }
      const tone = band.color
      return h(
        'section',
        { style: C.card },
        h(
          'div',
          { style: C.cardHead },
          t('watchdog.title'),
          h('span', { style: { ...C.chip, marginLeft: 8 } }, (watchdog.enforce ? t('watchdog.modeEnforce') : t('watchdog.mode'))),
          h(Pill, { tone: band.tone, text: `${t('watchdog.waterline')}: ${t(band.key)}` }),
        ),
        h(
          'div',
          { style: { height: 10, borderRadius: 999, background: 'var(--dsw-alias-border-l1, rgba(0,0,0,.08))', overflow: 'hidden', margin: '4px 0 10px' } },
          h('div', { style: { width: `${Math.round(ratio * 1000) / 10}%`, height: '100%', background: tone } }),
        ),
        h(
          'div',
          { style: C.grid },
          h('div', { style: C.key }, t('watchdog.heap')),
          h('div', { style: C.mono }, `${Math.round(heap.heapUsed / 1048576)} MB / ${Math.round(heap.heapLimit / 1048576)} MB`),
          h('div', { style: C.key }, t('watchdog.rss')),
          h('div', { style: C.mono }, `${Math.round((heap.rss || 0) / 1048576)} MB`),
          h('div', { style: C.key }, t('watchdog.thresholds')),
          h(
            'div',
            { style: C.mono },
            `${Math.round(display.totalWarn / 1048576)} / ${Math.round(display.totalPause / 1048576)} / ${Math.round(display.totalBlock / 1048576)} MB`,
          ),
        ),
      )
    }

    function PrisonCard({ watchdog }) {
      if (!watchdog) {
        return h(
          'section',
          { style: C.card },
          h('div', { style: C.cardHead }, t('prison.title')),
          h('p', { style: { ...C.muted, fontSize: 12, margin: 0 } }, t('watchdog.disabled')),
        )
      }
      const operations = Array.isArray(watchdog.operations) ? watchdog.operations.slice(0, 5) : []
      const incidents = Array.isArray(watchdog.incidents) ? watchdog.incidents.slice(-5).reverse() : []
      return h(
        'section',
        { style: C.card },
        h('div', { style: C.cardHead }, t('prison.title'), h('span', { style: { ...C.chip, marginLeft: 8 } }, (watchdog.enforce ? t('watchdog.modeEnforce') : t('watchdog.mode')))),
        operations.length === 0
          ? h('p', { style: { ...C.muted, fontSize: 12, margin: '0 0 6px' } }, t('prison.noestimates'))
          : h(
              'table',
              { style: C.table },
              h(
                'thead',
                null,
                h('tr', null, h('th', { style: C.th }, t('prison.top', { n: operations.length })), h('th', { style: C.th }, t('prison.estimate')), h('th', { style: C.th }, t('prison.ops'))),
              ),
              h(
                'tbody',
                null,
                operations.map((row) =>
                  h(
                    'tr',
                    { key: row.target },
                    h('td', { style: { ...C.td, ...C.mono, wordBreak: 'break-all' } }, row.target),
                    h('td', { style: C.td }, `${row.estimateMB} MB`),
                    h('td', { style: C.td }, String(row.count)),
                  ),
                ),
              ),
            ),
        incidents.length > 0
          ? h(
              'div',
              null,
              h('div', { style: { ...C.key, marginBottom: 4 } }, t('prison.incidents')),
              h(
                'div',
                { style: { display: 'flex', flexDirection: 'column', gap: 4 } },
                incidents.map((incident, index) =>
                  h(
                    'div',
                    { key: `${incident.at || index}-${index}`, style: { ...C.mono, fontSize: 11.5, wordBreak: 'break-word' } },
                    `${incident.at || ''} · ${incident.kind || 'event'}${incident.detail ? ` · ${incident.detail}` : ''}${incident.target ? ` · ${incident.target}` : ''}`,
                  ),
                ),
              ),
            )
          : null,
      )
    }

    function EnforceCard({ watchdog, refresh }) {
      const [actionError, setActionError] = React.useState(null)
      if (!watchdog || !watchdog.ok) return null
      const act = (name, target, prompt) => {
        if (prompt && !window.confirm(prompt)) return
        setActionError(null)
        postWatchdogAction(name, target)
          .then(() => refresh())
          .catch((cause) => {
            setActionError(String((cause && cause.message) || cause))
            refresh()
          })
      }
      const toggleHeap6g = (enabled) => {
        setActionError(null)
        postWatchdogAction('heap6g', '', { enabled })
          .then(() => refresh())
          .catch((cause) => {
            setActionError(String((cause && cause.message) || cause))
            refresh()
          })
      }
      const frozen = Boolean(watchdog.frozenAt) || watchdog.waterline === 'frost'
      const paused = Boolean(watchdog.pausedAt) || watchdog.waterline === 'pause'
      const grace = Number.isFinite(watchdog.graceUntil) && Date.now() < watchdog.graceUntil
      const quarantine = Array.isArray(watchdog.quarantine) ? watchdog.quarantine : []
      const topFive = Array.isArray(watchdog.topFive) ? watchdog.topFive : []
      const crashPlan = watchdog.crashPlan || null
      const plans = Array.isArray(watchdog.plans) ? watchdog.plans.slice(-2).reverse() : []
      const button = { cursor: 'pointer', padding: '3px 9px', borderRadius: 6, border: '1px solid var(--dsw-alias-border-l1, rgba(0,0,0,.18))', background: 'transparent', fontSize: 12 }
      return h('section', { style: C.card },
        h('div', { style: C.cardHead }, t('watchdog.enforceTitle'), h('span', { style: { ...C.chip, marginLeft: 8 } }, watchdog.enforce ? t('watchdog.enforceOn') : t('watchdog.observeOnly'))),
        actionError ? h('p', { style: { color: C.error.color, fontSize: 12.5, margin: '0 0 8px', wordBreak: 'break-word' } }, t('watchdog.actionError') + ': ' + actionError) : null,
        frozen ? h('p', { style: { color: C.error.color, fontSize: 12.5, margin: '0 0 8px' } }, t('watchdog.freezeHint', { level: watchdog.waterline })) : null,
        paused && !grace ? h('div', { style: { margin: '0 0 10px' } }, h('button', { style: { ...button, padding: '6px 12px' }, onClick: () => act('confirm-pause') }, t('watchdog.confirmPause'))) : null,
        h('div', { style: { ...C.key, marginBottom: 4 } }, t('watchdog.quarantine')),
        quarantine.length === 0
          ? h('p', { style: { ...C.muted, fontSize: 12, margin: '0 0 10px' } }, t('watchdog.noQuarantine'))
          : h('div', { style: { display: 'flex', flexDirection: 'column', gap: 8, marginBottom: 10 } },
              quarantine.map((entry) => h('div', { key: entry.target, style: { display: 'flex', alignItems: 'flex-start', gap: 8, flexWrap: 'wrap' } },
                h('div', { style: { display: 'flex', flexDirection: 'column', gap: 2, flex: '1 1 260px', minWidth: 0 } },
                  h('span', { style: { ...C.mono, fontSize: 12, wordBreak: 'break-all' } }, entry.target + ' · ' + entry.reason + ' · ' + entry.level),
                  entry.remediation ? h('div', { style: { fontSize: 11.5, color: 'var(--dsw-alias-label-secondary, #61666b)', lineHeight: 1.5 } },
                    h('div', { style: { fontWeight: 600 } }, entry.remediation.title),
                    h('div', null, entry.remediation.why),
                    h('div', null, entry.remediation.impact),
                    entry.remediation.steps.map((step, index) => h('div', { key: index }, (index + 1) + '. ' + step)),
                    h('div', null, '解除：' + entry.remediation.release),
                  ) : null,
                ),
                h('button', { style: button, onClick: () => act('release', entry.target, '假释 ' + entry.target + ' ？') }, t('watchdog.release')),
              ))),
        topFive.length === 0 ? null : h('div', null,
          h('div', { style: { ...C.key, marginBottom: 4 } }, t('watchdog.topFive', { n: topFive.length })),
          h('table', { style: C.table },
            h('thead', null, h('tr', null,
              h('th', { style: C.th }, t('prison.plugin')),
              h('th', { style: C.th }, t('prison.estimate')),
              h('th', { style: C.th }, t('prison.rss')),
              h('th', { style: C.th }, t('watchdog.memoryTier')),
              h('th', { style: C.th }, ''),
            )),
            h('tbody', null, topFive.map((row) => h('tr', { key: row.target },
              h('td', { style: { ...C.td, ...C.mono, fontSize: 11.5, wordBreak: 'break-all' } }, row.target),
              h('td', { style: C.td }, row.estimateMB + ' MB'),
              h('td', { style: C.td }, (row.rssMB || 0) + ' MB'),
              h('td', { style: C.td }, t(MEMORY_TIER_KEYS[row.memoryTier] || MEMORY_TIER_KEYS.ok) + (row.memoryMB != null ? ' · ' + row.memoryMB + ' MB' : '')),
              h('td', { style: C.td }, h('button', { style: button, onClick: () => act('close', row.target, '立即缉拿 ' + row.target + ' ？') }, t('watchdog.close'))),
            ))),
          ),
        ),
        Array.isArray(watchdog.thirdPartyPending) && watchdog.thirdPartyPending.length > 0
          ? h('p', { style: { ...C.muted, fontSize: 11.5, margin: '6px 0 0' } },
              t('watchdog.thirdPartyPending', { n: watchdog.thirdPartyPending.length })
              + ' ' + watchdog.thirdPartyPending.map((entry) => entry.name + (entry.mb != null ? ' (' + entry.mb + ' MB)' : '')).join(', '))
          : null,
        crashPlan ? h('div', { style: { marginTop: 12, paddingTop: 10, borderTop: '1px dashed var(--dsw-alias-border-l1, rgba(0,0,0,.12))' } },
          h('div', { style: { ...C.key, marginBottom: 4 } }, t('crashPlan.title')),
          h('div', { style: { fontSize: 12.5, marginBottom: 4 } },
            t('crashPlan.level', { level: crashPlan.level })
              + ' · ' + t('crashPlan.window', { n: crashPlan.crashes, minutes: Math.round((crashPlan.windowMs || 0) / 60000) })
              + (crashPlan.nextLevel ? ' · ' + t('crashPlan.next', { level: crashPlan.nextLevel }) : ' · ' + t('crashPlan.max'))),
          h('div', { style: { ...C.muted, fontSize: 11.5, lineHeight: 1.6 } },
            ['isolate-target', 'revert-changed', 'safe-mode'].map((step) => {
              const active = Array.isArray(crashPlan.steps) && crashPlan.steps.indexOf(step) >= 0
              return h('div', { key: step, style: { opacity: active ? 1 : 0.45 } }, (active ? '▸ ' : '· ') + t(CRASH_STEP_KEYS[step]))
            })),
          crashPlan.lastPlan
            ? h('div', { style: { ...C.mono, fontSize: 11, marginTop: 4 } }, t('crashPlan.last', {
                at: crashPlan.lastPlan.at || '—',
                target: crashPlan.lastPlan.target || crashPlan.lastPlan.fallback || 'unattributed',
                applied: crashPlan.lastPlan.applied ? t('crashPlan.applied') : t('crashPlan.notApplied'),
              }))
            : null,
        ) : null,
        plans.length === 0 ? null : h('div', { style: { marginTop: 10 } },
          h('div', { style: { ...C.key, marginBottom: 4 } }, t('watchdog.plans')),
          plans.map((plan) => h('div', { key: plan.at, style: { ...C.mono, fontSize: 11.5, wordBreak: 'break-word' } },
            plan.at + ' · ' + plan.level + ' · ' + (plan.target || 'unattributed') + (plan.inFlight ? ' (in-flight)' : '') + (plan.crash ? ' · ' + (plan.crash.oom ? 'OOM' : 'exit ' + plan.crash.hostExitCode) : ''))),
        ),
        watchdog.baseline ? h('p', { style: { ...C.muted, fontSize: 11.5, margin: '10px 0 0' } }, t('watchdog.baseline', { at: watchdog.baseline.at })) : null,
        watchdog.tamper ? h('p', { style: { ...C.muted, fontSize: 11.5, margin: '6px 0 0' } }, t('watchdog.tamper', { n: (watchdog.tamper.files || []).length, at: watchdog.tamper.lastCheckAt || '—', policy: watchdog.tamper.policy || '' })) : null,
        watchdog.pendingBoot && watchdog.pendingBoot.exists
          ? h('p', { style: { color: '#b45309', fontSize: 11.5, margin: '6px 0 0' } },
              t('watchdog.pendingBoot', { level: watchdog.pendingBoot.level || '?' })
              + ' · ' + t('watchdog.pendingBootConsumer', { consumer: watchdog.pendingBoot.consumer || 'none' })
              + (watchdog.pendingBoot.hint ? ' · ' + watchdog.pendingBoot.hint : '')
            )
          : null,
        h('div', { style: { marginTop: 10, display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' } },
          h('button', { style: button, onClick: () => toggleHeap6g(!(watchdog.heap6g || watchdog.heap6gRequest)) }, (watchdog.heap6g || watchdog.heap6gRequest) ? t('watchdog.heap6gOn') : t('watchdog.heap6gOff')),
          (watchdog.heap6g || watchdog.heap6gRequest) ? h('span', { style: { ...C.muted, fontSize: 11.5 } }, t('watchdog.heap6gPending')) : null,
        ),
        h('div', { style: { marginTop: 10, display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' } },
          h('button', { style: { ...button, padding: '5px 10px', opacity: frozen ? 0.5 : 1 }, disabled: frozen, title: frozen ? t('watchdog.measureBlocked') : undefined, onClick: () => { if (!frozen) act('probe') } }, t('watchdog.measure')),
          watchdog.probeProgress ? h('span', { style: { ...C.mono, fontSize: 11.5 } }, `${watchdog.probeProgress.done || 0}/${watchdog.probeProgress.total || 0}`) : null,
        ),
      )
    }

    function MatrixCard({ levels }) {
      if (!Array.isArray(levels) || levels.length === 0) {
        return h('section', { style: C.card }, h('div', { style: C.cardHead }, t('matrix.title')), h('p', { style: { ...C.muted, margin: 0, fontSize: 12 } }, t('matrix.empty')))
      }
      return h(
        'section',
        { style: C.card },
        h('div', { style: C.cardHead }, t('matrix.title')),
        h(
          'table',
          { style: C.table },
          h('thead', null, h('tr', null, h('th', { style: C.th }, t('matrix.code')), h('th', { style: C.th }, t('matrix.name')), h('th', { style: C.th }, t('matrix.duration')), h('th', { style: C.th }, t('matrix.deprivations')))),
          h(
            'tbody',
            null,
            levels.map((level) =>
              h(
                'tr',
                { key: level.code },
                h('td', { style: C.td }, h('span', { style: C.code }, level.code)),
                h('td', { style: C.td }, level.name),
                h('td', { style: C.td }, h('span', { title: level.durationHint || '' }, level.durationClass || '—')),
                h(
                  'td',
                  { style: C.td },
                  h('div', { style: C.chips }, (level.deprivations || []).map((item) => h('span', { key: item, style: C.chip }, item))),
                ),
              ),
            ),
          ),
        ),
      )
    }

    function LedgerCard({ ledger, cliFound, busy, onAction, lastResult }) {
      const [pending, setPending] = React.useState(null)
      const exists = Boolean(ledger && ledger.exists)
      const needsReset = exists && Number(ledger.segments ?? 0) > 0
      const integrityTone = !exists ? 'none' : ledger.integrity === 'ok' ? 'ok' : 'error'
      const integrityText = !exists ? t('integrity.none') : ledger.integrity === 'ok' ? t('integrity.ok') : t('integrity.failed')
      const entries = [
        { key: 'dir', label: t('ledger.dir'), value: h('span', { style: { ...C.mono, wordBreak: 'break-all' } }, ledger?.dir || '—') },
        { key: 'integrity', label: t('ledger.integrity'), value: h(Pill, { tone: integrityTone, text: integrityText }) },
      ]
      if (exists) {
        entries.push({ key: 'records', label: t('ledger.records'), value: h('span', { style: C.mono }, String(ledger.records ?? '—')) })
        entries.push({ key: 'segments', label: t('ledger.segments'), value: h('span', { style: C.mono }, String(ledger.segments ?? '—')) })
        entries.push({ key: 'files', label: t('ledger.files'), value: h('span', { style: C.mono }, String(ledger.files ?? '—')) })
      }
      if (exists && ledger.detail) {
        entries.push({ key: 'detail', label: t('ledger.detail'), value: h('span', { style: { ...C.mono, wordBreak: 'break-word' } }, ledger.detail) })
      }
      const disabled = Boolean(busy) || !cliFound
      /** A non-empty ledger is cleared first; that run needs a second click. */
      const run = (action) => {
        if (needsReset && pending !== action) {
          setPending(action)
          return
        }
        setPending(null)
        onAction(needsReset ? `reset-${action}` : action)
      }
      const labelOf = (action, idle) => {
        if (busy === action || busy === `reset-${action}`) return t('action.running')
        if (needsReset && pending === action) return t('action.confirmRun', { records: ledger.records ?? '?' })
        return t(idle)
      }
      const confirmStyle = (action) =>
        needsReset && pending === action ? { ...C.actionBtn, color: '#b45309', borderColor: '#f59e0b' } : C.actionBtn
      return h(
        'section',
        { style: C.card },
        h('div', { style: C.cardHead }, t('ledger.title')),
        h(
          'div',
          { style: C.grid },
          entries.flatMap((entry) => [
            h('div', { key: `k-${entry.key}`, style: C.key }, entry.label),
            h('div', { key: `v-${entry.key}` }, entry.value),
          ]),
        ),
        !exists ? h('p', { style: { ...C.muted, fontSize: 12, margin: '8px 0 0' } }, t('ledger.empty')) : null,
        h(
          'div',
          { style: { display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap', marginTop: 10 } },
          h('button', { type: 'button', style: confirmStyle('demo'), disabled, onClick: () => run('demo') }, labelOf('demo', 'action.runDemo')),
          h('button', { type: 'button', style: confirmStyle('drill'), disabled, onClick: () => run('drill') }, labelOf('drill', 'action.runDrill')),
          h('span', { style: { ...C.muted, fontSize: 11.5 } }, t('ledger.actionsHint')),
        ),
        needsReset ? h('p', { style: { ...C.muted, fontSize: 12, margin: '8px 0 0' } }, t('ledger.resetHint')) : null,
        lastResult
          ? h(
              'p',
              {
                style: {
                  ...C.mono,
                  fontSize: 11.5,
                  margin: '8px 0 0',
                  color: lastResult.ok ? C.ok.color : C.error.color,
                  wordBreak: 'break-word',
                },
              },
              `${t('ledger.lastAction')}: ${lastResult.action} — ${lastResult.summary}`,
            )
          : null,
      )
    }

    function PrisonPage() {
      const [data, setData] = React.useState(null)
      const [error, setError] = React.useState(null)
      const [loading, setLoading] = React.useState(true)
      const [updated, setUpdated] = React.useState(null)
      const [busy, setBusy] = React.useState(null)
      const [lastResult, setLastResult] = React.useState(null)
      const [watchdog, setWatchdog] = React.useState(null)
      const statusAbort = React.useRef(null)
      const statusInFlight = React.useRef(false)
      const watchdogAbort = React.useRef(null)
      const watchdogInFlight = React.useRef(false)

      const load = React.useCallback(() => {
        // A slow verify can exceed the 20s cadence; skip instead of stacking requests.
        if (statusInFlight.current) return
        statusInFlight.current = true
        const controller = new AbortController()
        statusAbort.current = controller
        setLoading(true)
        setError(null)
        fetchStatus(controller.signal)
          .then((payload) => {
            setData(payload)
            setUpdated(new Date())
          })
          .catch((cause) => {
            if (cause && cause.name === 'AbortError') return
            setError(String((cause && cause.message) || cause))
          })
          .finally(() => {
            if (statusAbort.current === controller) {
              statusAbort.current = null
              statusInFlight.current = false
              setLoading(false)
            }
          })
      }, [])

      React.useEffect(() => {
        load()
        const timer = setInterval(load, 20000)
        return () => {
          clearInterval(timer)
          if (statusAbort.current) statusAbort.current.abort()
        }
      }, [load])

      const refreshWatchdog = React.useCallback(() => {
        if (watchdogInFlight.current) return
        watchdogInFlight.current = true
        const controller = new AbortController()
        watchdogAbort.current = controller
        fetchWatchdog(controller.signal)
          .then((payload) => setWatchdog(payload))
          .catch((cause) => {
            if (cause && cause.name === 'AbortError') return
            setWatchdog(null)
          })
          .finally(() => {
            if (watchdogAbort.current === controller) {
              watchdogAbort.current = null
              watchdogInFlight.current = false
            }
          })
      }, [])

      React.useEffect(() => {
        refreshWatchdog()
        const timer = setInterval(refreshWatchdog, 15000)
        return () => {
          clearInterval(timer)
          if (watchdogAbort.current) watchdogAbort.current.abort()
        }
      }, [refreshWatchdog])

      const runAction = React.useCallback(
        (action) => {
          setBusy(action)
          setLastResult(null)
          postRun(action)
            .then((payload) => {
              setLastResult({ action, ok: true, summary: payload.summary || '' })
              setBusy(null)
              load()
            })
            .catch((cause) => {
              setLastResult({ action, ok: false, summary: String((cause && cause.message) || cause) })
              setBusy(null)
            })
        },
        [load],
      )

      const cli = data?.cli
      const cliText = cli && cli.found ? t('cli.found', { version: cli.version || '?' }) : t('cli.missing')
      const pillText = data && data.pluginVersion ? t('panel.version', { version: data.pluginVersion }) : cliText
      const timeText = updated ? updated.toLocaleTimeString() : ''

      return h(
        'div',
        { style: C.page },
        h(
          'div',
          { style: C.header },
          h('span', { style: { display: 'inline-flex' } }, h(PrisonIcon, { size: 26, active: true })),
          h('div', { style: { minWidth: 0 } }, h('h1', { style: C.title }, t('page.title')), h('p', { style: C.sub }, t('page.subtitle'))),
          h(Pill, { tone: cli && cli.found ? 'ok' : 'error', text: pillText }),
          h('button', { type: 'button', style: C.btn, onClick: load, disabled: loading }, loading ? t('action.loading') : t('action.refresh')),
        ),
        error
          ? h(
              'section',
              { style: C.card },
              h('div', { style: { ...C.cardHead, ...C.error } }, t('error.title')),
              h('p', { style: { ...C.mono, margin: 0 } }, error),
              h('p', { style: { ...C.muted, fontSize: 12, margin: '8px 0 0' } }, t('error.hint')),
            )
          : null,
        h(MatrixCard, { levels: data?.levels }),
        h(LedgerCard, {
          ledger: data?.ledger,
          cliFound: Boolean(cli && cli.found),
          busy,
          onAction: runAction,
          lastResult,
        }),
        h(MemoryCard, { watchdog }),
        h(PrisonCard, { watchdog }),
        h(EnforceCard, { watchdog, refresh: refreshWatchdog }),
        h(
          'p',
          { style: { ...C.muted, fontSize: 12, margin: '2px 0 0', display: 'flex', gap: 12, flexWrap: 'wrap' } },
          h('span', null, t('meta.readonly')),
          cli && cli.path ? h('span', { style: C.mono }, `${t('cli.source')}: ${cli.source} · ${cli.path} · aiprison ${cli.version || '?'}`) : null,
          timeText ? h('span', null, t('meta.updated', { time: timeText })) : null,
        ),
      )
    }

    function apply(ctx) {
      try {
        if (ctx.locale) {
          ctx.locale.register(NS, 'en', DICTS.en)
          ctx.locale.register(NS, 'zh', DICTS.zh)
          translate = ctx.locale.bind(NS)
        } else if (typeof ctx.inject === 'function') {
          ctx.inject(['locale'], (child) => {
            child.locale.register(NS, 'en', DICTS.en)
            child.locale.register(NS, 'zh', DICTS.zh)
            translate = child.locale.bind(NS)
          })
        }
      } catch (cause) {
        console.warn('[ai-prison] locale registration failed', cause)
      }
      ctx.effect(
        () =>
          ctx.slots.inject('sidebar.panellist', () =>
            ctx.slots.register({ name: 'sidebar.panellist', id: PANEL_ID, order: 2, label: () => t('panel.label') }, PrisonIcon),
          ),
        'ai-prison: sidebar entry',
      )
      ctx.effect(
        () => ctx.slots.inject('main', () => ctx.slots.register({ name: 'main', key: PANEL_ID }, PrisonPage)),
        'ai-prison: main panel',
      )
    }

    return { inject: ['slots'], apply, PANEL_ID, NS }
  },
})
