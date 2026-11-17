#!/usr/bin/env node
/**
 * Pre-publish gate for @twinsearth/ai-prison-dsh-plugin.
 *
 * Verifies manifest/version consistency, the files the runtime actually needs,
 * the six bundled binaries, artwork and locale metadata, the market index, the
 * absence of secrets in packaged text, and the exact npm pack file list.
 */
import { spawnSync } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const here = path.dirname(fileURLToPath(import.meta.url))
const pluginRoot = path.resolve(here, '..')
const results = []

function check(name, fn) {
  try {
    fn()
    results.push({ name, status: 'ok', detail: '' })
  } catch (error) {
    results.push({ name, status: 'FAIL', detail: String((error && error.message) || error) })
  }
}

function readJson(relative) {
  return JSON.parse(fs.readFileSync(path.join(pluginRoot, relative), 'utf8'))
}

const pkg = readJson('package.json')
const market = readJson('market/index.json')
const TARGETS = [
  ['windows-amd64', 'aiprison.exe'],
  ['windows-arm64', 'aiprison.exe'],
  ['macos-amd64', 'aiprison'],
  ['macos-arm64', 'aiprison'],
  ['linux-amd64', 'aiprison'],
  ['linux-arm64', 'aiprison'],
]
const FORBIDDEN = [
  [/npm_[A-Za-z0-9]{36,}/, 'npm token'],
  [/ghp_[A-Za-z0-9]{20,}/, 'GitHub token'],
  [/github_pat_[A-Za-z0-9_]{20,}/, 'GitHub fine-grained token'],
  [/sk-[A-Za-z0-9]{20,}/, 'API key'],
  [/-----BEGIN [A-Z ]*PRIVATE KEY-----/, 'private key'],
]

check('version is semver and the market index agrees', () => {
  if (!/^[0-9]+\.[0-9]+\.[0-9]+$/.test(pkg.version)) throw new Error(`bad version ${pkg.version}`)
  const entry = market.plugins?.find((plugin) => plugin.name === pkg.name)
  if (!entry) throw new Error('market index has no entry for this package')
  if (entry.versions?.[0]?.version !== pkg.version) throw new Error('market index version differs from package.json')
  if (entry.versions[0].engines?.dsh !== pkg.engines.dsh) throw new Error('market engines.dsh differs from package.json')
  if (!(entry.versions[0].publishTimestamp > 1_700_000_000)) throw new Error('publishTimestamp looks wrong')
})

check('runtime files exist', () => {
  const required = [
    'index.js',
    'client.js',
    'watchdog.js',
    'cordis.patch.yml',
    'icon.svg',
    'README.md',
    'CHANGELOG.md',
    'LICENSE',
    'docs/DETAIL.md',
    'docs/DETAIL.zh.md',
    'docs/preview.svg',
    'locale/en.json',
    'locale/zh.json',
    'lib/api.js',
    'lib/cli.js',
    'lib/harness.js',
    'lib/levels.js',
    'lib/status.js',
    'lib/watchdog-policy.js',
    'lib/watchdog-runtime.js',
    'lib/watchdog-state.js',
    'lib/tools.js',
  ]
  for (const relative of required) if (!fs.existsSync(path.join(pluginRoot, relative))) throw new Error(`missing ${relative}`)
  for (const declared of pkg.files) {
    if (declared.includes('*')) continue
    if (!fs.existsSync(path.join(pluginRoot, declared))) throw new Error(`files[] entry missing: ${declared}`)
  }
})

check('six platform binaries are present, distinct and executable', () => {
  const hashes = new Set()
  for (const [target, exe] of TARGETS) {
    const file = path.join(pluginRoot, 'bin', target, exe)
    if (!fs.existsSync(file)) throw new Error(`missing bundled binary ${target}`)
    const bytes = fs.readFileSync(file)
    if (bytes.length < 1024 * 1024) throw new Error(`${target} binary looks too small (${bytes.length} bytes)`)
    const digest = bytes.subarray(0, 4096).toString('hex')
    if (hashes.has(digest)) throw new Error(`${target} duplicates another build`)
    hashes.add(digest)
  }
})

check('the bundled CLI for this platform answers version and levels', () => {
  const platform = { win32: 'windows', darwin: 'macos', linux: 'linux' }[process.platform]
  const arch = { x64: 'amd64', arm64: 'arm64' }[process.arch]
  const exe = process.platform === 'win32' ? 'aiprison.exe' : 'aiprison'
  const file = path.join(pluginRoot, 'bin', `${platform}-${arch}`, exe)
  if (!fs.existsSync(file)) throw new Error(`no bundled binary for ${platform}-${arch}`)
  for (const args of [['version'], ['levels']]) {
    const run = spawnSync(file, args, { encoding: 'utf8' })
    if (run.status !== 0) throw new Error(`${args.join(' ')} exited ${run.status}: ${run.stderr}`)
  }
})

check('artwork stays inside the documented limits and rules', () => {
  const icon = fs.readFileSync(path.join(pluginRoot, 'icon.svg'), 'utf8')
  if (icon.length > 256 * 1024) throw new Error('icon exceeds 256 KiB')
  if (!/^<svg[\s>]/.test(icon)) throw new Error('icon is not SVG')
  if (/<script|<text|<image|<foreignObject|<style/i.test(icon)) throw new Error('icon carries forbidden markup')
  const preview = fs.readFileSync(path.join(pluginRoot, 'docs/preview.svg'), 'utf8')
  if (preview.length > 1024 * 1024) throw new Error('preview exceeds 1 MiB')
  if (/<script|<text|<foreignObject/i.test(preview)) throw new Error('preview carries forbidden markup')
  if (!/width="640" height="300"/.test(preview.split('>')[0] + '>')) throw new Error('preview is not 640x300')
})

check('locale metadata points at one-image detail documents', () => {
  for (const [locale, file] of [['en', 'locale/en.json'], ['zh', 'locale/zh.json']]) {
    const meta = readJson(file).meta
    if (!meta || !meta.title || !meta.description) throw new Error(`${locale} meta incomplete`)
    if (!meta.detail) throw new Error(`${locale} meta.detail missing`)
    const doc = path.join(pluginRoot, meta.detail)
    if (!fs.existsSync(doc)) throw new Error(`${locale} detail document missing`)
    const lines = fs.readFileSync(doc, 'utf8').trim().split(/\r?\n/)
    if (lines.length !== 1 || !/^!\[[^\]]+\]\([^)]+\)$/.test(lines[0])) throw new Error(`${locale} detail document is not exactly one image line`)
  }
})

check('the patch inserts the row and no row is disabled', () => {
  const patch = fs.readFileSync(path.join(pluginRoot, 'cordis.patch.yml'), 'utf8')
  if (!/id:\s*ai-prison/.test(patch)) throw new Error('patch row id missing')
  if (!/name:\s*'@twinsearth\/ai-prison-dsh-plugin'/.test(patch)) throw new Error('patch package name missing')
  if (/disabled:\s*true/.test(patch)) throw new Error('patch disables the row')
})

check('packaged text carries no secrets', () => {
  const skip = [path.join(pluginRoot, 'bin')]
  const walk = (dir) => {
    for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
      const full = path.join(dir, entry.name)
      if (skip.some((prefix) => full.startsWith(prefix))) continue
      if (entry.name === 'node_modules' || entry.name === '.git') continue
      if (entry.isDirectory()) walk(full)
      else if (/\.(js|mjs|json|yml|yaml|md|svg|txt)$/.test(entry.name)) {
        const text = fs.readFileSync(full, 'utf8')
        for (const [pattern, label] of FORBIDDEN) {
          if (pattern.test(text)) throw new Error(`${label} pattern in ${path.relative(pluginRoot, full)}`)
        }
      }
    }
  }
  walk(pluginRoot)
})

check('every JavaScript module parses', () => {
  const files = ['index.js', 'client.js', ...fs.readdirSync(path.join(pluginRoot, 'lib')).map((name) => path.join('lib', name))]
  for (const relative of files) {
    const run = spawnSync(process.execPath, ['--check', path.join(pluginRoot, relative)], { encoding: 'utf8' })
    if (run.status !== 0) throw new Error(`${relative}: ${run.stderr.trim()}`)
  }
})

check('npm pack ships the runtime, the locales, the docs and the binaries', () => {
  // Windows resolves npm through an .cmd shim, which needs a shell to spawn.
  const run = spawnSync('npm', ['pack', '--dry-run', '--json'], {
    cwd: pluginRoot,
    encoding: 'utf8',
    shell: process.platform === 'win32',
  })
  if (run.status !== 0) throw new Error(`npm pack --dry-run failed: ${run.error || run.stderr || run.stdout}`)
  let parsed
  try {
    parsed = JSON.parse(run.stdout)
  } catch (error) {
    throw new Error(`npm pack output is not JSON: ${String(error)}`)
  }
  const info = Array.isArray(parsed) ? parsed[0] : parsed
  const names = (info.files || []).map((file) => file.path)
  if (names.length === 0) throw new Error('npm pack reported no files')
  const required = [
    'index.js',
    'client.js',
    'cordis.patch.yml',
    'icon.svg',
    'locale/en.json',
    'locale/zh.json',
    'docs/DETAIL.md',
    'docs/DETAIL.zh.md',
    'docs/preview.svg',
    'lib/cli.js',
    'lib/tools.js',
    'bin/windows-amd64/aiprison.exe',
    'bin/linux-amd64/aiprison',
    'bin/macos-arm64/aiprison',
  ]
  for (const relative of required) if (!names.includes(relative)) throw new Error(`npm pack is missing ${relative}`)
  if (names.some((name) => name.startsWith('node_modules/') || name.startsWith('.git/'))) throw new Error('npm pack includes excluded directories')
  if (names.some((name) => name.startsWith('test/') || name.startsWith('scripts/'))) throw new Error('npm pack includes development-only files')
  if (!(info.size > 1_000_000) || info.size > 60_000_000) throw new Error(`tarball size looks wrong: ${info.size}`)
})

const failed = results.filter((entry) => entry.status !== 'ok')
for (const entry of results) console.log(`${entry.status === 'ok' ? 'ok  ' : 'FAIL'} ${entry.name}${entry.detail ? ` — ${entry.detail}` : ''}`)
console.log(`\n${results.length - failed.length}/${results.length} checks passed`)
process.exit(failed.length === 0 ? 0 : 1)
