import { spawn } from 'node:child_process'

/**
 * Optional desktop notification for enforcement events. Uses the Windows tray
 * balloon through PowerShell so no extra dependency is needed. Disabled by
 * default; enable with `config: { notify: true }` on the watchdog row.
 */
const KIND_LABELS = {
  freeze: '内存冻结',
  pause: '内存暂停',
  quarantine: '插件隔离',
  tamper: '配置篡改',
  'crash-enforced': '崩溃隔离',
  budget: '内存超限',
}

let lastAt = 0
let lastKind = ''

export function shouldNotify(kind, now = Date.now(), cooldownMs = 60_000) {
  if (!KIND_LABELS[kind]) return false
  if (kind === lastKind && now - lastAt < cooldownMs) return false
  lastKind = kind
  lastAt = now
  return true
}

export function notificationFor(incident) {
  const kind = incident && incident.kind
  const label = KIND_LABELS[kind]
  if (!label) return null
  const target = incident.target ? ` ${incident.target}` : ''
  const title = `AI Prison · ${label}`
  const body = String(incident.detail || incident.evidence || target || '').slice(0, 180) || label
  return { title, body }
}

function powershellScript(title, body) {
  const safeTitle = String(title).replace(/'/g, "''")
  const safeBody = String(body).replace(/'/g, "''")
  return [
    'Add-Type -AssemblyName System.Windows.Forms | Out-Null',
    'Add-Type -AssemblyName System.Drawing | Out-Null',
    '$n = New-Object System.Windows.Forms.NotifyIcon',
    '$n.Icon = [System.Drawing.SystemIcons]::Warning',
    '$n.Visible = $true',
    `$n.ShowBalloonTip(8000, '${safeTitle}', '${safeBody}', [System.Windows.Forms.ToolTipIcon]::Warning)`,
    'Start-Sleep -Seconds 9',
    '$n.Visible = $false',
    '$n.Dispose()',
  ].join('; ')
}

/** Fire one tray notification (best effort, detached). */
export function notify(incident, { cooldownMs = 60_000 } = {}) {
  const message = notificationFor(incident)
  if (!message) return { ok: false, reason: 'unsupported-kind' }
  if (process.platform !== 'win32') return { ok: false, reason: 'platform' }
  const now = Date.now()
  if (!shouldNotify(incident.kind, now, cooldownMs)) return { ok: false, reason: 'rate-limited' }
  lastAt = now
  lastKind = incident.kind
  try {
    const child = spawn('powershell.exe', ['-NoProfile', '-ExecutionPolicy', 'Bypass', '-Command', powershellScript(message.title, message.body)], {
      detached: true,
      stdio: 'ignore',
      windowsHide: true,
    })
    child.unref()
    return { ok: true, title: message.title }
  } catch (error) {
    return { ok: false, reason: String((error && error.message) || error) }
  }
}

/** Test hook. */
export function resetNotifierState() {
  lastAt = 0
  lastKind = ''
}
