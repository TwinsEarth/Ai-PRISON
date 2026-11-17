/**
 * The CLI prints the matrix as human text; this turns it back into data:
 *
 *   L0 Warning            [immediate] immediate, appealable
 *       - reputation deduction
 *       - record flag
 */
const HEADER = /^([A-Z][0-9])\s+([^\s[][^[]*?)\s+\[([^\]]+)\]\s*(.*)$/
const ITEM = /^\s+[-*]\s+(.*)$/

export function parseLevels(text) {
  const levels = []
  let current
  for (const raw of String(text || '').split(/\r?\n/)) {
    const line = raw.replace(/\s+$/, '')
    if (line === '') continue
    const header = line.match(HEADER)
    if (header) {
      current = {
        code: header[1],
        name: header[2].trim(),
        durationClass: header[3].trim(),
        durationHint: header[4].trim(),
        deprivations: [],
      }
      levels.push(current)
      continue
    }
    const item = line.match(ITEM)
    if (item && current) current.deprivations.push(item[1].trim())
  }
  return levels
}

/** One line per level, for tool results and compact logs. */
export function summarizeLevels(levels) {
  return (levels || []).map((level) => `${level.code} ${level.name} [${level.durationClass}] ${level.deprivations.length} 项剥夺`)
}
