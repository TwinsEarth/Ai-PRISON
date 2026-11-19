import fs from 'node:fs'
import { fileURLToPath } from 'node:url'

/**
 * Version of the installed plugin package.
 *
 * The panel must show the bundle version (which follows releases), not the
 * bundled aiprison CLI version (which is the Go tool's own 1.0.x line).
 */
export const PLUGIN_VERSION = (() => {
  try {
    const manifest = fileURLToPath(new URL('../package.json', import.meta.url))
    return JSON.parse(fs.readFileSync(manifest, 'utf8')).version || null
  } catch {
    return null
  }
})()
