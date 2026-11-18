/**
 * Child-process RSS attribution shared by the watchdog and its tests.
 * The query lists every process with a command line; a process belongs to a
 * plugin when its command line references that plugin's package directory.
 */
export const RSS_QUERY_SCRIPT = [
  "$ErrorActionPreference='SilentlyContinue'",
  'Get-CimInstance Win32_Process | Where-Object { $_.CommandLine } | ForEach-Object { ($_.ProcessId.ToString() + \'|\' + [string]$_.WorkingSetSize + \'|\' + $_.CommandLine) }',
].join('; ')

export function parseProcessList(output, pluginNames, { selfPid = process.pid, skipHostMarker = 'dsh-desktop-host' } = {}) {
  const names = [...new Set((Array.isArray(pluginNames) ? pluginNames : []).filter(Boolean))]
  const rss = {}
  let lines = 0
  let matched = 0
  for (const line of String(output || '').split('\n')) {
    const trimmed = line.trim()
    if (!trimmed) continue
    const parts = trimmed.split('|')
    if (parts.length < 3) continue
    lines += 1
    const pid = Number(parts[0])
    const bytes = Number(parts[1]) || 0
    const commandLine = parts.slice(2).join('|')
    if (pid === selfPid || commandLine.includes(skipHostMarker)) continue
    for (const name of names) {
      const winNeedle = 'node_modules' + '\\' + name.split('/').join('\\')
      const posixNeedle = 'node_modules/' + name
      if (commandLine.includes(winNeedle) || commandLine.includes(posixNeedle)) {
        rss[name] = (rss[name] || 0) + bytes
        matched += 1
        break
      }
    }
  }
  return { rss, lines, matched }
}
