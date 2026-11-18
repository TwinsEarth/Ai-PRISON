import * as nodeModule from 'node:module'
import { createRequire } from 'node:module'
import { pathToFileURL } from 'node:url'
import path from 'node:path'

// Child side of the AI Prison per-plugin budget probe (see budget-probe.js).
// argv: [node, --expose-gc, thisFile, profileDir, packageName, installDir]
const [, , profileDir, packageName, installDir] = process.argv

function heapMB() {
  return process.memoryUsage().heapUsed / (1024 * 1024)
}

// Plugins import peer packages such as @deepseek-ai/dsh-session that live in the
// DSH installation, not in the profile. Resolve those from the install root so
// the probe can measure the real entry module.
if (installDir && typeof nodeModule.registerHooks === 'function') {
  try {
    const requireFromInstall = createRequire(path.join(installDir, 'package.json'))
    nodeModule.registerHooks({
      resolve(specifier, context, nextResolve) {
        if (specifier.startsWith('@deepseek-ai/') || specifier === 'cordis') {
          try {
            return { url: pathToFileURL(requireFromInstall.resolve(specifier)).href, shortCircuit: true }
          } catch {
            // fall through to the default resolver
          }
        }
        return nextResolve(specifier, context)
      },
    })
  } catch {
    // hooks are best effort; the probe still measures what resolves normally
  }
}

async function main() {
  if (!profileDir || !packageName) {
    console.log(JSON.stringify({ ok: false, error: 'missing-args' }))
    return
  }
  if (typeof global.gc !== 'function') {
    console.log(JSON.stringify({ ok: false, error: 'no-gc' }))
    return
  }
  let entry
  try {
    const require = createRequire(`${profileDir}/package.json`)
    entry = require.resolve(packageName)
  } catch (error) {
    try {
      entry = import.meta.resolve(packageName)
    } catch {
      console.log(JSON.stringify({ ok: false, error: `unresolved: ${String((error && error.message) || error)}` }))
      return
    }
  }
  global.gc()
  const before = heapMB()
  const started = Date.now()
  try {
    await import(pathToFileURL(entry).href)
  } catch (error) {
    console.log(JSON.stringify({ ok: false, error: `import-failed: ${String((error && error.message) || error).slice(0, 300)}` }))
    return
  }
  global.gc()
  const after = heapMB()
  console.log(JSON.stringify({
    ok: true,
    moduleLoadMB: Math.max(0, Math.round(after - before)),
    rssMB: Math.round(process.memoryUsage().rss / (1024 * 1024)),
    ms: Date.now() - started,
    entry,
  }))
}

await main()
