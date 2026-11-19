/**
 * Human-readable remediation for every AI Prison enforcement action.
 *
 * Each entry answers four questions in fixed order so the panel and the agent
 * say the same thing: why it happened, what it affects, what to do, and how the
 * plugin gets out of prison.
 */
const REMEDIATION = {
  'hot-ops': {
    title: '插件热操作过频',
    why: '同一插件在本次运行中被反复安装、启用或热切换（超过 5 次），插件图被反复重组，是内存暴涨与崩溃的高风险来源。',
    impact: '该插件已被自动关闭并隔离；本次运行内不再加载。',
    steps: [
      '确认你确实需要反复开关它；安装/升级完成一次即可，不要连续点击。',
      '重启 DSH（或在本面板点"假释"）后重新启用一次。',
      '如果它每次启动都反复启用自己，向插件作者反馈"启动期重复 enable"。',
    ],
    release: '重启 DSH 会自动释放；也可以在面板"插件监狱"里点"假释"手动释放。',
  },
  crash: {
    title: '该插件被归因为崩溃来源',
    why: 'DSH 宿主进程崩溃（如爆内存），崩溃日志里的 hostRunId 与本插件最后一次安装/启用记录匹配。',
    impact: '该插件已被隔离并关闭，隔离在重启后仍生效，避免崩溃循环。',
    steps: [
      '先重启 DSH，确认在没有该插件时环境稳定。',
      '检查插件版本是否有修复（升级到最新版），或查看其 README 的已知崩溃问题。',
      '确需继续使用时，在面板"插件监狱"点"假释"，一次只放行一个插件并观察内存。',
    ],
    release: '仅手动释放（面板"假释"）；重启不会自动解除，防止崩溃循环。',
  },
  budget: {
    title: '启动内存超过硬阈值',
    why: '隔离探针实测该插件模块加载的 retained 内存超过 500 MB 硬阈值（或该项目历史上留下超限记录）。',
    impact: '该插件已被关闭并隔离，且写入 pending-boot.json，下次启动前会被外部 guard 直接剔除。',
    steps: [
      '升级到该插件的修复版本；若仍超限，联系作者或换用轻量替代。',
      '如果确认是误报（插件版本已变化），在面板"插件监狱"点"假释"后重新测量预算。',
      '临时需要使用时，只放行一次并在面板观察内存 Top5。',
    ],
    release: '手动释放（面板"假释"）；释放后再测一次预算确认。',
  },
  tamper: {
    title: '关键配置文件被非插件管理器修改',
    why: 'profile 的 package.json / cordis.patch.yml / pnpm-workspace.yaml / pnpm-lock.yaml 在插件管理器操作窗口之外发生了变化（被修改或删除）。',
    impact: '默认 observe 只记录并保存证据，不会改动文件；策略为 restore 时才会把被改动内容备份到 .ai-prison-watchdog/tampered/、恢复原文件，并隔离归因到的最后安装/启用插件。',
    steps: [
      '检查 .ai-prison-watchdog/tampered/ 下的备份，确认改动来自哪里（手工编辑、脚本、其他工具）。',
      '不要在 DSH 运行中手工改 profile 配置；确需修改时先退出 DSH。',
      '确认需要自动恢复时，显式把 tamperPolicy 设为 restore，并先核对备份内容。',
      '确认无插件行为异常后，在面板"插件监狱"点"假释"恢复被隔离的插件。',
    ],
    release: '手动释放（面板"假释"）；恢复前先核对 tampered/ 备份。',
  },
  freeze: {
    title: '内存水位达到冻结线',
    why: '宿主堆内存超过 5 GB 冻结阈值（6 GB host；4 GB host 按比例折算），继续加载/重组插件会直接导致 OOM 崩溃。',
    impact: '所有增内存的插件操作被禁止（安装、启用、热切换、重载、卸载等），Agent 的 plugin_manager 也已被拒绝。',
    steps: [
      '在面板"内存看护 → Top5"里立即关闭占用最大的插件。',
      '关闭后等待 1 分钟让堆回落，或直接重启 DSH。',
      '排查是谁在持续吃内存：优先怀疑最近开启/升级的插件。',
    ],
    release: '堆回落到冻结线以下并持续 60 秒会自动解除；或重启 DSH。',
  },
  pause: {
    title: '内存水位达到暂停线，需要二次确认',
    why: '宿主堆内存超过 3.9 GB 暂停阈值，插件操作已暂停，防止继续推高水位。',
    impact: '所有增内存的插件操作被临时拦截；本次操作需要你明确确认。',
    steps: [
      '先在"内存看护 → Top5"里关闭一个不需要的插件。',
      '确认要继续时，点面板的"我已知晓：允许插件操作 10 分钟"。',
      '宽限期内若水位继续上涨到 5 GB，会自动升级为冻结。',
    ],
    release: '点"二次确认"给 10 分钟宽限；堆回落到 3.6 GB 以下会自动解除。',
  },
}

/** Look up the remediation for one enforcement reason. */
export function remediationFor(reason, extra = {}) {
  const code = REMEDIATION[reason] ? reason : 'hot-ops'
  const base = REMEDIATION[code]
  return {
    code,
    title: base.title,
    why: base.why,
    impact: base.impact,
    steps: [...base.steps],
    release: base.release,
    ...(extra.count !== undefined ? { detail: `本次运行计数 ${extra.count}` } : {}),
  }
}

/** Reasons this module knows about (used by tests and the panel). */
export const REMEDIATION_REASONS = Object.keys(REMEDIATION)
