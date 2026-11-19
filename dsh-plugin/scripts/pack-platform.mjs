#!/usr/bin/env node
/**
 * Build platform-lite npm tarballs for the bundled aiprison CLI.
 *
 * The published package carries six CLI builds (~8.8 MB packed). Most users only
 * need one, so this script copies the package to a temporary directory, keeps
 * only the selected `bin/<platform>-<arch>/` tree, runs `npm pack` there and
 * renames the tarball with the target suffix. `--all` produces the six assets in
 * one run (used by the release workflow and for offline installs).
 *
 * Usage (from dsh-plugin/):
 *   node scripts/pack-platform.mjs --platform windows --arch amd64
 *   node scripts/pack-platform.mjs --all --out ../dist/platform-packs
 *   node scripts/pack-platform.mjs --all --dry-run
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

export function parseArgs(argv, { root = pluginRoot } = {}) {
  const args = {
    platform: 'windows',
    arch: 'amd64',
    all: false,
    dryRun: false,
    out: path.join(root, '..', 'dist', 'platform-packs'),
    root,
  }
  for (let index = 0; index < argv.length; index += 1) {
    const current = argv[index]
    if (current === '--all') args.all = true
    else if (current === '--dry-run') args.dryRun = true
    else if (current === '--platform') args.platform = argv[++index]
    else if (current === '--arch') args.arch = argv[++index]
    else if (current === '--out') args.out = path.resolve(argv[++index])
    else throw new Error(`unknown argument: ${current}`)
  }
  const target = `${args.platform}-${args.arch}`
  if (!BUNDLED_TARGETS.includes(target)) throw new Error(`unknown target ${target}; expected one of ${BUNDLED_TARGETS.join(', ')}`)
  return { ...args, target, targets: args.all ? [...BUNDLED_TARGETS] : [target] }
}

function copyInto(targetDir, relative, root) {
  const source = path.join(root, relative)
  const destination = path.join(targetDir, relative)
  if (!fs.existsSync(source)) return false
  fs.mkdirSync(path.dirname(destination), { recursive: true })
  fs.cpSync(source, destination, { recursive: true })
  return true
}

function executableOf(target) {
  return target.startsWith('windows') ? 'aiprison.exe' : 'aiprison'
}

function binaryOf(root, target) {
  return path.join(root, 'bin', target, executableOf(target))
}

export function packOne({ target, out, dryRun, root = pluginRoot }) {
  const binSource = binaryOf(root, target)
  if (!fs.existsSync(binSource)) throw new Error(`missing bundled CLI: ${binSource}`)
  const packedBytes = fs.statSync(binSource).size
  const skippedTargets = BUNDLED_TARGETS.filter((entry) => entry !== target).length
  if (dryRun) return { target, dryRun: true, platformBinaryMB: Math.round(packedBytes / 1024 / 1024 * 10) / 10, skippedTargets }

  const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'ai-prison-pack-'))
  try {
    for (const file of COPY) copyInto(temp, file, root)
    for (const directory of COPY_DIRS) copyInto(temp, directory, root)
    copyInto(temp, path.join('bin', target), root)
    fs.mkdirSync(out, { recursive: true })
    const result = spawnSync('npm', ['pack', '--pack-destination', out], { cwd: temp, encoding: 'utf8', shell: process.platform === 'win32' })
    if (result.status !== 0) throw new Error(`npm pack failed for ${target}: ${result.stderr || result.stdout}`)
    const packedName = String(result.stdout || '').trim().split(/\r?\n/).find((line) => line.endsWith('.tgz'))
    if (!packedName) throw new Error(`npm pack did not report a tarball name for ${target}`)
    // Target suffix keeps lite tarballs from overwriting the all-platform one.
    const targetFile = path.join(out, packedName.replace(/\.tgz$/, `-${target}.tgz`))
    fs.renameSync(path.join(out, packedName), targetFile)
    const bytes = fs.statSync(targetFile).size
    return {
      target,
      tarball: targetFile,
      sizeMB: Math.round(bytes / 1024 / 1024 * 10) / 10,
      platformBinaryMB: Math.round(packedBytes / 1024 / 1024 * 10) / 10,
      skippedTargets,
    }
  } finally {
    fs.rmSync(temp, { recursive: true, force: true })
  }
}

export function main(argv = process.argv.slice(2), { root = pluginRoot } = {}) {
  const args = parseArgs(argv, { root })
  const results = args.targets.map((target) => packOne({ target, out: args.out, dryRun: args.dryRun, root }))
  if (results.length === 1) {
    console.log(JSON.stringify(results[0], null, 2))
    return results[0]
  }
  const totalMB = results.reduce((sum, result) => sum + (result.sizeMB || 0), 0)
  console.log(JSON.stringify({ ok: true, out: args.out, count: results.length, totalMB: Math.round(totalMB * 10) / 10, targets: results }, null, 2))
  return results
}

const invoked = process.argv[1] ? path.resolve(process.argv[1]) : ''
if (invoked === fileURLToPath(import.meta.url)) main()
