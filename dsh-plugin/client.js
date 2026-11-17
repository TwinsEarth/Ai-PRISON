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

    const DICTS = {
      en: {
        'panel.label': 'AI Prison',
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
        'action.runDemo': 'Run demo (create ledger)',
        'action.runDrill': 'Run recovery drill',
        'action.resetDemo': 'Rebuild ledger (clear and run demo)',
        'action.confirmReset': 'Click again to clear {records} records',
        'action.running': 'Running…',
        'ledger.actions': 'Explicit actions',
        'ledger.actionsHint': 'These buttons run the bundled CLI and write the ledger; the panel never writes on its own.',
        'ledger.demoNeedsEmpty': 'The ledger is not empty: the demo needs a fresh ledger. Rebuild it or run the recovery drill instead.',
        'ledger.lastAction': 'Last action',
        'meta.updated': 'Updated {time}',
        'meta.readonly': 'Read-only view: the panel never writes by itself; only the buttons above do.',
        'error.title': 'Cannot read the status',
        'error.hint': 'Check that the bundle is enabled and the aiprison CLI resolves for this platform.',
      },
      zh: {
        'panel.label': 'AI Prison',
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
        'action.runDemo': '运行 demo（创建账本）',
        'action.runDrill': '运行恢复演练',
        'action.resetDemo': '重置账本并重建',
        'action.confirmReset': '再次点击确认清空 {records} 条记录',
        'action.running': '运行中…',
        'ledger.actions': '显式操作',
        'ledger.actionsHint': '这些按钮会运行内置 CLI 并写入账本；面板本身不会自动写入。',
        'ledger.demoNeedsEmpty': '账本非空：demo 需要空账本。请点「重置账本并重建」，或改用「运行恢复演练」。',
        'ledger.lastAction': '最近操作',
        'meta.updated': '更新于 {time}',
        'meta.readonly': '只读视图：面板不会自行写入账本，只有上方按钮会写入。',
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
      page: { maxWidth: 980, margin: '0 auto', padding: '20px 24px 16px', color: 'var(--dsw-alias-label-primary, #0f1115)' },
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

    function Pill({ tone, text }) {
      const color = tone === 'ok' ? C.ok.color : tone === 'error' ? C.error.color : C.muted.color
      return h('span', { style: C.pill }, h('span', { style: { ...C.dot, background: color } }), h('span', null, text))
    }

    function fetchStatus() {
      return fetch(new URL(STATUS_URL, document.baseURI), {
        credentials: 'same-origin',
        headers: { accept: 'application/json' },
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
      const [confirmReset, setConfirmReset] = React.useState(false)
      const exists = Boolean(ledger && ledger.exists)
      const canDemo = !exists || ledger.canDemo !== false
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
      const rebuild = () => {
        if (!confirmReset) {
          setConfirmReset(true)
          return
        }
        setConfirmReset(false)
        onAction('reset-demo')
      }
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
          h(
            'button',
            { type: 'button', style: C.actionBtn, disabled: disabled || !canDemo, onClick: () => onAction('demo') },
            busy === 'demo' ? t('action.running') : t('action.runDemo'),
          ),
          h(
            'button',
            { type: 'button', style: C.actionBtn, disabled, onClick: () => onAction('drill') },
            busy === 'drill' ? t('action.running') : t('action.runDrill'),
          ),
          exists
            ? h(
                'button',
                {
                  type: 'button',
                  style: confirmReset ? { ...C.actionBtn, color: '#b45309', borderColor: '#f59e0b' } : C.actionBtn,
                  disabled,
                  onClick: rebuild,
                },
                busy === 'reset-demo'
                  ? t('action.running')
                  : confirmReset
                    ? t('action.confirmReset', { records: ledger.records ?? '?' })
                    : t('action.resetDemo'),
              )
            : null,
          h('span', { style: { ...C.muted, fontSize: 11.5 } }, t('ledger.actionsHint')),
        ),
        !canDemo ? h('p', { style: { ...C.muted, fontSize: 12, margin: '8px 0 0' } }, t('ledger.demoNeedsEmpty')) : null,
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

      const load = React.useCallback(() => {
        setLoading(true)
        setError(null)
        fetchStatus()
          .then((payload) => {
            setData(payload)
            setUpdated(new Date())
            setLoading(false)
          })
          .catch((cause) => {
            setError(String((cause && cause.message) || cause))
            setLoading(false)
          })
      }, [])

      React.useEffect(() => {
        load()
        const timer = setInterval(load, 20000)
        return () => clearInterval(timer)
      }, [load])

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
      const timeText = updated ? updated.toLocaleTimeString() : ''

      return h(
        'div',
        { style: C.page },
        h(
          'div',
          { style: C.header },
          h('span', { style: { display: 'inline-flex' } }, h(PrisonIcon, { size: 26, active: true })),
          h('div', { style: { minWidth: 0 } }, h('h1', { style: C.title }, t('page.title')), h('p', { style: C.sub }, t('page.subtitle'))),
          h(Pill, { tone: cli && cli.found ? 'ok' : 'error', text: cliText }),
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
        h(
          'p',
          { style: { ...C.muted, fontSize: 12, margin: '2px 0 0', display: 'flex', gap: 12, flexWrap: 'wrap' } },
          h('span', null, t('meta.readonly')),
          cli && cli.path ? h('span', { style: C.mono }, `${t('cli.source')}: ${cli.source} · ${cli.path}`) : null,
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
