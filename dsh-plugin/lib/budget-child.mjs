import { createRequire } from 'node:module'
import { pathToFileURL } from 'node:url'

// Child side of the AI Prison per-plugin budget probe (see budget-probe.js).
// argv: [node, --expose-gc, thisFile, profileDir, packageName]
const [, , profileDir, packageName] = process.argv

function heapMB() {
  return process.memoryUsage().heapUsed / (1024 * 1024)
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
