#!/usr/bin/env node
/**
 * Build the six platform binaries bundled into the npm package.
 *
 * Run from the repository root: `node dsh-plugin/scripts/build-binaries.mjs`.
 * The Go project is pure standard library, so every target cross-compiles with
 * CGO disabled and no module downloads.
 */
import { spawnSync } from 'node:child_process'
import { existsSync, mkdirSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const here = path.dirname(fileURLToPath(import.meta.url))
const pluginRoot = path.resolve(here, '..')
const repoRoot = path.resolve(pluginRoot, '..')

export const TARGETS = [
  { goos: 'windows', goarch: 'amd64', dir: 'windows-amd64', exe: 'aiprison.exe' },
  { goos: 'windows', goarch: 'arm64', dir: 'windows-arm64', exe: 'aiprison.exe' },
  { goos: 'darwin', goarch: 'amd64', dir: 'macos-amd64', exe: 'aiprison' },
  { goos: 'darwin', goarch: 'arm64', dir: 'macos-arm64', exe: 'aiprison' },
  { goos: 'linux', goarch: 'amd64', dir: 'linux-amd64', exe: 'aiprison' },
  { goos: 'linux', goarch: 'arm64', dir: 'linux-arm64', exe: 'aiprison' },
]

function git(...args) {
  const result = spawnSync('git', args, { cwd: repoRoot, encoding: 'utf8' })
  if (result.status !== 0) return ''
  return String(result.stdout || '').trim()
}

/**
 * The CLI version belongs to the Go project, not to the plugin: a plugin tag
 * such as `dsh-plugin-v1.1.0` must not leak into `aiprison version`. Prefer the
 * declared default in cmd/aiprison/main.go, then any `vX.Y.Z` tag reachable from
 * HEAD (plugin tags are excluded by the pattern).
 */
function toolVersion() {
  if (process.env.AIPRISON_VERSION) return process.env.AIPRISON_VERSION
  try {
    const main = fs.readFileSync(path.join(repoRoot, 'cmd', 'aiprison', 'main.go'), 'utf8')
    const declared = main.match(/\bversion\s*=\s*"([0-9][^"]*)"/)
    if (declared) return declared[1]
  } catch {
    // fall through to the tag lookup
  }
  const tag = git('describe', '--tags', '--match', 'v[0-9]*', '--abbrev=0')
  if (tag.startsWith('v')) return tag.slice(1)
  return '1.0.0'
}

function buildTime() {
  return new Date().toISOString().replace(/\.\d+Z$/, 'Z')
}

function main() {
  if (!existsSync(path.join(repoRoot, 'cmd', 'aiprison'))) {
    console.error(`找不到 Go CLI 源码：${path.join(repoRoot, 'cmd', 'aiprison')}`)
    process.exit(1)
  }
  const version = process.env.AIPRISON_VERSION || toolVersion()
  const commit = process.env.AIPRISON_COMMIT || git('rev-parse', '--short', 'HEAD') || 'unknown'
  const built = process.env.AIPRISON_BUILD_TIME || buildTime()
  const ldflags = `-s -w -X main.version=${version} -X main.commit=${commit} -X main.buildTime=${built}`

  for (const target of TARGETS) {
    const outDir = path.join(pluginRoot, 'bin', target.dir)
    mkdirSync(outDir, { recursive: true })
    const outFile = path.join(outDir, target.exe)
    const args = ['build', '-trimpath', '-ldflags', ldflags, '-o', outFile, './cmd/aiprison']
    const result = spawnSync('go', args, {
      cwd: repoRoot,
      stdio: 'inherit',
      env: { ...process.env, GOOS: target.goos, GOARCH: target.goarch, CGO_ENABLED: '0' },
    })
    if (result.status !== 0) {
      console.error(`构建 ${target.dir} 失败（exit ${result.status}）`)
      process.exit(result.status || 1)
    }
    console.log(`built bin/${target.dir}/${target.exe} (aiprison ${version})`)
  }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) main()
