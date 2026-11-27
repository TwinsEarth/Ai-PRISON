#!/usr/bin/env node
/**
 * `npm test` runs on a clean checkout where `dsh-plugin/bin/` is gitignored.
 * Build just the host binary before the suite so CLI-backed tests are real;
 * the six-target release check stays a separate, explicit step. If no Go
 * toolchain is available, leave the bin absent and let the tests skip loudly
 * instead of failing with a misleading red.
 */
import { spawnSync } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { TARGETS } from './build-binaries.mjs'

const here = path.dirname(fileURLToPath(import.meta.url))
const pluginRoot = path.resolve(here, '..')
const platform = { win32: 'windows', darwin: 'macos', linux: 'linux' }[process.platform]
const arch = { x64: 'amd64', arm64: 'arm64' }[process.arch]
const target = TARGETS.find((t) => t.goos === platform && t.goarch === arch)

if (!target) {
  console.warn(`ensure-bin: no bundled target for ${process.platform}/${process.arch}; CLI tests will skip`)
  process.exit(0)
}

const out = path.join(pluginRoot, 'bin', target.dir, target.exe)
if (fs.existsSync(out)) {
  console.log(`ensure-bin: ${path.relative(pluginRoot, out)} already present`)
  process.exit(0)
}

const probe = spawnSync('go', ['version'], { encoding: 'utf8' })
if (probe.status !== 0) {
  console.warn('ensure-bin: go toolchain not found; CLI-backed tests will skip')
  process.exit(0)
}

console.log(`ensure-bin: building ${target.dir}/${target.exe}`)
const build = spawnSync(process.execPath, [path.join(here, 'build-binaries.mjs')], {
  cwd: path.resolve(pluginRoot, '..'),
  stdio: 'inherit',
  env: { ...process.env, AIPRISON_ONLY_TARGET: target.dir },
})
if (build.status !== 0) {
  // Some sandboxes report a non-zero Go exit for a directory-cleanup warning
  // after a successful link. The artifact is the source of truth.
  if (!fs.existsSync(out)) process.exit(build.status || 1)
  console.warn(`ensure-bin: go exited ${build.status}, but ${out} exists; continuing`)
}
if (!fs.existsSync(out)) {
  console.error(`ensure-bin: build reported success but ${out} is missing`)
  process.exit(1)
}
