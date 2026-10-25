#!/usr/bin/env node
/*
 * postinstall: best-effort download of the prebuilt aiprison binary for the
 * current platform from the public GitHub Release, sha256-verified against
 * the release checksums.txt.
 *
 * Failure modes (offline, asset not yet published, unsupported platform) all
 * print a clear warning and EXIT 0 — `npm install` must never block because
 * the Go binary could not be fetched. The user can install the binary later
 * from https://github.com/TwinsEarth/ai-prison/releases/latest.
 */
'use strict';

const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const https = require('node:https');
const crypto = require('node:crypto');
const { execFileSync } = require('node:child_process');

const BASE = 'https://github.com/TwinsEarth/ai-prison/releases/download/v1.0.0/';

function warn(msg) {
  console.warn(`[aiprison] ${msg}`);
}

function mapPlatform() {
  switch (process.platform) {
    case 'linux': return 'linux';
    case 'darwin': return 'darwin';
    case 'win32': return 'windows';
    default: return null;
  }
}

function mapArch() {
  switch (process.arch) {
    case 'x64': return 'amd64';
    case 'arm64': return 'arm64';
    default: return null;
  }
}

function fetch(url, redirectsLeft = 5) {
  return new Promise((resolve, reject) => {
    const req = https.get(url, { headers: { 'User-Agent': 'aiprison-npm-postinstall' } }, (res) => {
      if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
        res.resume();
        if (redirectsLeft <= 0) return reject(new Error('too many redirects'));
        return resolve(fetch(new URL(res.headers.location, url).toString(), redirectsLeft - 1));
      }
      if (res.statusCode !== 200) {
        res.resume();
        return reject(new Error(`HTTP ${res.statusCode} for ${url}`));
      }
      const chunks = [];
      res.on('data', (c) => chunks.push(c));
      res.on('end', () => resolve(Buffer.concat(chunks)));
      res.on('error', reject);
    });
    req.on('error', reject);
  });
}

function sha256(buf) {
  return crypto.createHash('sha256').update(buf).digest('hex');
}

async function main() {
  const p = mapPlatform();
  const a = mapArch();
  if (!p || !a) {
    warn(`unsupported platform ${process.platform}/${process.arch} — skipping binary download.`);
    warn('Build from source: https://github.com/TwinsEarth/ai-prison');
    return;
  }

  const isWindows = p === 'windows';
  const asset = isWindows ? `aiprison_${p}_${a}.zip` : `aiprison_${p}_${a}.tar.gz`;
  const vendorDir = path.join(__dirname, 'vendor');
  fs.mkdirSync(vendorDir, { recursive: true });

  let assetBuf;
  let sumsText;
  try {
    warn(`downloading ${asset} (release v1.0.0) ...`);
    assetBuf = await fetch(BASE + asset);
    sumsText = (await fetch(BASE + 'checksums.txt')).toString('utf8');
  } catch (e) {
    warn(`binary download failed: ${e.message}`);
    warn('Re-run `npm install` with network access, or grab the asset manually from:');
    warn('  ' + BASE);
    return;
  }

  // Verify sha256 against checksums.txt ("<64hex>  <filename>").
  const expected = sumsText.split(/\r?\n/)
    .map((line) => line.trim().split(/\s+/))
    .find((parts) => parts.length >= 2 && parts[1] === asset);
  if (!expected) {
    warn(`checksums.txt has no entry for ${asset} — skipping install.`);
    return;
  }
  if (expected[0].toLowerCase() !== sha256(assetBuf)) {
    warn(`sha256 mismatch for ${asset} — refusing to install.`);
    return;
  }
  warn('checksum OK');

  const binName = isWindows ? 'aiprison.exe' : 'aiprison';
  const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'aiprison-'));
  try {
    const archive = path.join(tmpDir, asset);
    fs.writeFileSync(archive, assetBuf);

    if (isWindows) {
      // Windows 10+ ships bsdtar as tar.exe, which handles .zip natively.
      try {
        execFileSync('tar', ['-xf', archive, '-C', tmpDir], { stdio: 'ignore' });
      } catch (_) {
        execFileSync('powershell', [
          '-NoProfile', '-Command',
          `Expand-Archive -LiteralPath '${archive}' -DestinationPath '${tmpDir}' -Force`,
        ], { stdio: 'ignore' });
      }
    } else {
      execFileSync('tar', ['-xzf', archive, '-C', tmpDir], { stdio: 'ignore' });
    }

    const dest = path.join(vendorDir, binName);
    fs.copyFileSync(path.join(tmpDir, binName), dest);
    if (!isWindows) fs.chmodSync(dest, 0o755);
    warn(`installed ${binName} -> ${dest}`);
  } catch (e) {
    warn(`extraction failed: ${e.message}`);
    warn('You can install the binary manually from: ' + BASE);
  } finally {
    fs.rmSync(tmpDir, { recursive: true, force: true });
  }
}

main().catch((e) => warn(`postinstall error: ${e.message}`));
// Always exit 0: npm install must not fail because of the optional binary.
process.exitCode = 0;
