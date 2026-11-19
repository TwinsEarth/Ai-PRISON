#!/usr/bin/env node
/**
 * Build a platform-lite npm tarball for one target.
 *
 * The published package bundles six CLI builds (~8.8 MB packed); most users only
 * need one. This script copies the package to a temporary directory, keeps only
 * `bin/<platform>-<arch>/`, and runs `npm pack` there, so a per-platform release
 * asset can be produced without changing the main package layout.
 *
 * Usage (from dsh-plugin/):
 *   node scripts/pack-platform.mjs --platform windows --arch amd64
 *   node scripts/pack-platform.mjs --platform macos --arch arm64 --out ../dist --dry-run
 */
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { spawnSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import { BUNDLED_TARGETS } from '../lib/cli.js'

const pluginRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')

const COPY = ['index.js', 'client.js', 'watchdog.js', 'cordis.patch.yml', 'icon.svg', 'README.md', 'CHANGELOG.md', 'LICENSE', 'package.json']
const COPY_DIRS = ['lib', 'locale', 'docs']

function parseArgs(argv) {
  const args = { platform: 'windows', arch: 'amd64', out: path.join(pluginRoot, '..', 'dist'), dryRun: false }
  for (let index = 0; index < argv.length; index += 1) {
    const current = argv[index]
    if (current === '--dry-run') args.dryRun = true
    else if (current === '--platform') args.platform = argv[++index]
    else if (current === '--arch') args.arch = argv[++index]
    else if (current === '--out') args.out = path.resolve(argv[++index])
    else throw new Error(`unknown argument: ${current}`)
  }
  const target = `${args.platform}-${args.arch}`
  if (!BUNDLED_TARGETS.includes(target)) throw new Error(`unknown target ${target}; expected one of ${BUNDLED_TARGETS.join(', ')}`)
  return { ...args, target }
}

function copyInto(targetDir, relative) {
  const source = path.join(pluginRoot, relative)
  const destination = path.join(targetDir, relative)
  if (!fs.existsSync(source)) return false
  fs.mkdirSync(path.dirname(destination), { recursive: true })
  fs.cpSync(source, destination, { recursive: true })
  return true
}

function main() {
  const { platform, arch, target, out, dryRun } = parseArgs(process.argv.slice(2))
  const executable = platform === 'windows' ? 'aiprison.exe' : 'aiprison'
  const binSource = path.join(pluginRoot, 'bin', target, executable)
  if (!fs.existsSync(binSource)) throw new Error(`missing bundled CLI: ${binSource}`)

  const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'ai-prison-pack-'))
  try {
    for (const file of COPY) copyInto(temp, file)
    for (const directory of COPY_DIRS) copyInto(temp, directory)
    copyInto(temp, path.join('bin', target))
    const packagedBytes = fs.statSync(binSource).size
    const otherBins = BUNDLED_TARGETS.filter((entry) => entry !== target).length
    if (dryRun) {
      console.log(JSON.stringify({ dryRun: true, target, temp, platformBinaryMB: Math.round(packagedBytes / 1024 / 1024 * 10) / 10, skippedTargets: otherBins }, null, 2))
      return
    }
    fs.mkdirSync(out, { recursive: true })
    const result = spawnSync('npm', ['pack', '--pack-destination', out], { cwd: temp, encoding: 'utf8', shell: process.platform === 'win32' })
    if (result.status !== 0) throw new Error(`npm pack failed: ${result.stderr || result.stdout}`)
    const lines = String(result.stdout || '').trim().split(/\r?\n/)
    const packedName = lines.find((line) => line.endsWith('.tgz'))
    if (!packedName) throw new Error('npm pack did not report a tarball name')
    // Keep the platform suffix so a lite tarball never overwrites the full one.
    const targetFile = path.join(out, packedName.replace(/\.tgz$/, `-${target}.tgz`))
    fs.renameSync(path.join(out, packedName), targetFile)
    const bytes = fs.statSync(targetFile).size
    const allBinariesMB = BUNDLED_TARGETS.reduce((total, entry) => {
      const executableName = entry.startsWith('windows') ? 'aiprison.exe' : 'aiprison'
      const candidate = path.join(pluginRoot, 'bin', entry, executableName)
      return total + (fs.existsSync(candidate) ? fs.statSync(candidate).size : 0)
    }, 0) / 1024 / 1024
    console.log(JSON.stringify({
      ok: true,
      target,
      tarball: targetFile,
      sizeMB: Math.round(bytes / 1024 / 1024 * 10) / 10,
      platformBinaryMB: Math.round(packagedBytes / 1024 / 1024 * 10) / 10,
      allBinariesMB: Math.round(allBinariesMB * 10) / 10,
      skippedTargets: otherBins,
    }, null, 2))
  } finally {
    fs.rmSync(temp, { recursive: true, force: true })
  }
}

main()
