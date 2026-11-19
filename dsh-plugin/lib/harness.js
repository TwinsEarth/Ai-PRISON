import path from 'node:path'
import { createRequire } from 'node:module'
import { pathToFileURL } from 'node:url'

/**
 * Candidate anchors for the running harness installation. The Desktop app ships
 * one asar archive, while CLI layouts use a plain package directory; every
 * candidate is tried with createRequire because module resolution inside an asar
 * only works in the Electron-as-Node runtime that launched DSH.
 */
export function installationAnchors({ execPath = process.execPath, env = process.env, argv = process.argv } = {}) {
  const dir = path.dirname(execPath)
  // The desktop host may run on the bundled Node (real 6 GB heap) with the
  // runtime extracted to disk. Then execPath no longer sits next to the app and
  // the installation root arrives as DSH_INSTALL_DIR or as the host's second
  // argument (after the entry module).
  const argvRoot = typeof argv[2] === 'string' && argv[2].endsWith('package.json') === false ? argv[2] : undefined
  // The entry module (argv[1]) often sits either directly in the runtime root
  // (…/app.asar/dsh/index.js) or in a subdirectory (…/dsh/dist/index.js). Both
  // are useful anchors when execPath no longer points at the installation.
  const entry = typeof argv[1] === 'string' && path.isAbsolute(argv[1]) ? argv[1] : undefined
  const entryDir = entry ? path.dirname(entry) : undefined
  const entryParent = entryDir ? path.dirname(entryDir) : undefined
  const entryLooksLikeDsh = (candidate) => Boolean(candidate) && /(^|[\\/])(app\.asar|dsh|dsh-runtime)([\\/]|$)/.test(candidate)
  return [
    env.DSH_APP_ANCHOR,
    env.DSH_INSTALL_DIR ? path.join(env.DSH_INSTALL_DIR, 'package.json') : undefined,
    argvRoot && path.basename(argvRoot) === 'dsh' ? path.join(argvRoot, 'package.json') : undefined,
    entryLooksLikeDsh(entryDir) ? path.join(entryDir, 'package.json') : undefined,
    entryLooksLikeDsh(entryParent) ? path.join(entryParent, 'package.json') : undefined,
    path.join(dir, 'resources', 'app.asar', 'dsh', 'package.json'),
    path.join(dir, 'resources', 'app', 'dsh', 'package.json'),
    path.join(dir, '..', 'Resources', 'app.asar', 'dsh', 'package.json'),
    path.join(dir, '..', 'Resources', 'app', 'dsh', 'package.json'),
  ].filter(Boolean)
}

/**
 * Import the harness' own copy of a core package. A plugin must register tools
 * through the exact module instance the harness validated, so resolve it from
 * the installation rather than bundling a second copy.
 */
export async function loadHarnessModule(specifier, { anchors = installationAnchors() } = {}) {
  for (const anchor of anchors) {
    let file
    try {
      const require_ = createRequire(pathToFileURL(anchor))
      file = require_.resolve(specifier)
    } catch {
      continue
    }
    try {
      const module = await import(pathToFileURL(file).href)
      return { module, file, anchor }
    } catch {
      // Keep trying other anchors.
    }
  }
  return undefined
}

/** The `defineTool` builder from @deepseek-ai/dsh-tools, when resolvable. */
export async function loadDefineTool(options = {}) {
  const found = await loadHarnessModule('@deepseek-ai/dsh-tools', options)
  const defineTool = found && found.module && found.module.defineTool
  return typeof defineTool === 'function' ? { defineTool, file: found.file } : undefined
}
