# Supply chain: official tool API, signing and provenance

Status: strategy document, 2026-11-19. Code signing needs a certificate/upstream
decision; everything in “Implemented now” already ships with 1.7.2.

## 1. Official Host API for tool registration

Fact from the installed DSH version (`cordis_inspect_query` → `tools` service):

```ts
register(definition: ToolDefinition, options?: ToolRegistrationOptions): () => void;
```

`ToolDefinition` is a plain object: `name`, `description`, `parameters`,
`output` and `execute`. `defineTool` from `@deepseek-ai/dsh-tools` is not part of
the required seam.

What the plugin does now:

1. `ctx.inject(['tools'])` → `child.tools.register(plainDefinition)` — official
   path, no package-path guessing.
2. Only if that throws (older harness build) it falls back to
   `defineTool(definition)` resolved through `lib/harness.js`.
3. The path-guessing anchors therefore no longer gate tool registration; they
   are a best-effort compatibility fallback and never the primary code path.

Bundled packages resolve from the DSH installation and profile bundles from the
profile, so a bundle must not guess `@deepseek-ai/*` locations from
`$DSH_PROFILE_DIR` (see the installed DSH plugin-development documentation).
`lib/harness.js` remains only for the legacy fallback and is covered by tests.

## 2. Signing strategy (needs a certificate / upstream decision)

| Target | Recommended option | Requirements | Status |
|---|---|---|---|
| Windows `aiprison.exe` (6 builds) | Azure Trusted Signing (cheaper, CI-native) or an OV/EV Authenticode certificate; SignPath.io offers free OSS signing | Azure account/subscription or CA-issued cert; `signtool` with timestamp server | not signed |
| macOS `aiprison` | Apple Developer ID Application + `notarytool` (hardened runtime), staple the ticket | Apple Developer Program (~99 USD/year) | not signed/not notarized |
| Linux `aiprison` | Detached signature: `minisign`/`cosign` over `SHA256SUMS`, or upstream distro signing | maintainer key, published public key | checksums only |
| npm tarball | npm registry integrity (`sha512`) + GitHub Release `SHA256SUMS` | none (already done) | done |
| CI artifacts | GitHub artifact attestation (`actions/attest-build-provenance`) | public repo; `id-token`/`attestations` permissions | added in 1.7.2 workflow |
| npm provenance | `npm publish --provenance` from CI with OIDC | publish must move from a maintainer laptop into CI (currently manual due 2FA) | not enabled |

No private key belongs in the repository. Signing steps run in CI with secrets
(`WINDOWS_CERT_*`, `APPLE_ID_*`, `APPLE_TEAM_ID`, `APPLE_APP_PASSWORD` or a
self-hosted signer); the repo only documents and verifies the result.

### Verification users can run today

```bash
# npm integrity (registry)
npm view @twinsearth/ai-prison-dsh-plugin@1.7.2 dist.integrity

# GitHub Release checksum
curl -LO https://github.com/TwinsEarth/Ai-PRISON/releases/download/dsh-plugin-v1.7.2/SHA256SUMS
sha256sum -c SHA256SUMS   # or: Get-FileHash <tgz> -Algorithm SHA256

# per-platform lite tarball (unsigned, local build)
node dsh-plugin/scripts/pack-platform.mjs --platform windows --arch amd64
```

### Recommended rollout once a certificate exists

1. Windows: sign all six `aiprison.exe` in `scripts/build-binaries.mjs` before
   the tarball is packed; verify with `Get-AuthenticodeSignature`.
2. macOS: codesign + notarize the two Mach-O builds, staple and verify with
   `codesign --verify --strict` / `spctl -a`.
3. Publish `SHA256SUMS` plus a detached `SHA256SUMS.minisig`.
4. Move `npm publish` into the tag workflow with `--provenance` (removes the
   manual 2FA step) once the release guard/approval policy allows it.
5. Add a CI check that fails when a Windows/macOS artifact is unsigned.

## 3. What remains open (honest list)

- No code-signing certificate is available to this project yet; the binaries
  remain unsigned, and the README “Limitations” notes the Controlled Folder
  Access / SmartScreen consequences.
- Official-API adoption is implemented (plain `register` first); the legacy
  `defineTool` fallback can be removed after the minimum supported DSH version
  is known to accept plain definitions everywhere.
- npm provenance requires publishing from CI, which conflicts with the current
  manual/2FA publish flow; decide whether to move publishing.

## 4. Full-platform distribution strategy (P2-5)

**Current (implemented in the release workflow):**

| Channel | Artifact | Size |
|---|---|---|
| npm | one tarball with all six `bin/<platform>-<arch>/` trees | ~7.1 MB tgz (~8.8 MB unpacked) |
| GitHub Release | the same all-platform tarball **plus six lite tarballs** `<name>-<version>-<platform>-<arch>.tgz` | ~1.4–1.6 MB each |
| GitHub Release | `SHA256SUMS` covering the full tarball **and** the six lite ones | — |
| GitHub Release | artifact attestation over all tarballs + `SHA256SUMS` | — |

Lite tarballs are built with `scripts/pack-platform.mjs --all`; each keeps only
one binary tree and still contains the same Host/Client code, locale, docs and
manifest. They are meant for offline/air-gapped machines:

```bash
# on the target machine, after downloading the matching lite tarball
npm install ./twinsearth-ai-prison-dsh-plugin-<version>-windows-amd64.tgz
```

Measured with 1.7.2: windows-amd64 1.56 MB, windows-arm64 1.40 MB,
macos-amd64 1.52 MB, macos-arm64 1.41 MB, linux-amd64 1.50 MB, linux-arm64 1.37 MB
(six together 8.8 MB, i.e. the same bytes as one all-platform tarball).

**Not done yet (needs a decision):** splitting the npm package itself into a
meta-package plus six `@twinsearth/ai-prison-cli-<platform>` optional
dependencies (the esbuild/swc pattern). That would cut a normal `npm install`
to ~1.5 MB, but it requires publishing seven packages, keeping their versions in
lockstep, and reworking the release/2FA flow. The all-platform npm package stays
the default until that is explicitly approved.
