#!/usr/bin/env node
/*
 * aiprison npm shim: locates the platform binary fetched by install.js
 * (../vendor/) and execs it transparently — stdio, args, exit code pass through.
 */
'use strict';

const fs = require('node:fs');
const path = require('node:path');
const { spawnSync } = require('node:child_process');

const binName = process.platform === 'win32' ? 'aiprison.exe' : 'aiprison';
const candidates = [
  path.join(__dirname, '..', 'vendor', binName),
  path.join(__dirname, binName),
];
const bin = candidates.find((c) => fs.existsSync(c));

if (!bin) {
  console.error('aiprison: platform binary is not installed.');
  console.error('Re-run `npm install` with network access, or download it manually from:');
  console.error('  https://github.com/TwinsEarth/ai-prison/releases/latest');
  process.exit(127);
}

const result = spawnSync(bin, process.argv.slice(2), { stdio: 'inherit' });
if (result.error) {
  console.error('aiprison: failed to execute binary:', result.error.message);
  process.exit(127);
}
process.exit(result.status == null ? 1 : result.status);
