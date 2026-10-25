#!/usr/bin/env bash
# Pre-release allow-list gate.
#
# Every check prints [PASS] / [FAIL]. The script exits non-zero if any check
# fails. Intentionally read-mostly: `go mod tidy` runs against a backed-up
# go.mod/go.sum and is restored afterwards (no diff left behind).
set -euo pipefail

cd "$(dirname "$0")/.."

fail=0
ok()  { echo "[PASS] $1"; }
bad() { echo "[FAIL] $1"; fail=1; }

# --- 1. required root files / workflows / npm / scripts -----------------
required="README.md LICENSE CHANGELOG.md CONTRIBUTING.md SECURITY.md CODE_OF_CONDUCT.md go.mod Makefile npm/package.json scripts/build-release.sh"
for f in ${required}; do
  if [ -e "${f}" ]; then ok "root entry present: ${f}"; else bad "missing required root entry: ${f}"; fi
done
for w in ci.yml compat.yml release.yml; do
  if [ -f ".github/workflows/${w}" ]; then ok "workflow present: .github/workflows/${w}"; else bad "missing workflow: .github/workflows/${w}"; fi
done

# --- 2. version consistency (four places) -------------------------------
want="1.0.0"
if grep -Eq '^[[:space:]]*version[[:space:]]*=[[:space:]]*"1\.0\.0"' cmd/aiprison/main.go; then
  ok "CLI version = \"${want}\" (cmd/aiprison/main.go)"
else
  bad "cmd/aiprison/main.go: var version is not \"${want}\""
fi

# CHANGELOG must contain a release section. NOTE: if the current CHANGELOG
# still has everything under "## [Unreleased]", folding it into
# "## [1.0.0]" is a release-time step done before tagging — this check then
# flips to PASS.
if grep -Eq '^## \['"${want}"'\]' CHANGELOG.md; then
  ok "CHANGELOG.md has section ## [${want}]"
else
  bad "CHANGELOG.md has no '## [${want}]' section (fold [Unreleased] -> [${want}] before tagging)"
fi

if grep -Eq '"version"[[:space:]]*:[[:space:]]*"1\.0\.0"' npm/package.json; then
  ok "npm/package.json version = \"${want}\""
else
  bad "npm/package.json version is not \"${want}\""
fi

if git rev-parse "v${want}" >/dev/null 2>&1; then
  ok "git tag v${want} exists"
else
  bad "git tag v${want} does not exist yet (create after CHANGELOG fold)"
fi

# --- 3. code hygiene ----------------------------------------------------
if [ -z "$(gofmt -l .)" ]; then
  ok "gofmt -l . is clean"
else
  bad "gofmt violations:"
  gofmt -l . || true
fi

if go build ./...; then
  ok "go build ./... succeeds"
else
  bad "go build ./... failed"
fi

# go mod tidy must be a no-op. Backup, tidy, diff-check, always restore.
cp go.mod /tmp/aiprison-releasecheck-go.mod.bak
sum_backup=""
if [ -f go.sum ]; then
  cp go.sum /tmp/aiprison-releasecheck-go.sum.bak
  sum_backup="/tmp/aiprison-releasecheck-go.sum.bak"
fi
restore() {
  cp /tmp/aiprison-releasecheck-go.mod.bak go.mod
  rm -f /tmp/aiprison-releasecheck-go.mod.bak
  if [ -n "${sum_backup}" ]; then
    cp "${sum_backup}" go.sum
    rm -f "${sum_backup}"
  fi
}
trap restore EXIT

go mod tidy
if [ -z "$(git status --porcelain go.mod go.sum)" ]; then
  ok "go mod tidy leaves no diff on go.mod/go.sum"
else
  bad "go mod tidy produced a diff on go.mod/go.sum"
  git diff go.mod go.sum || true
fi

# --- 4. no hardcoded secrets -------------------------------------------
# Real secret shapes only. GitHub Actions expressions like
# ${{ secrets.GITHUB_TOKEN }} are intentional and are excluded by the
# grep -v filter below.
secret_hits=$(grep -rInE \
  --exclude-dir=.git --exclude-dir=dist --exclude-dir=staging --exclude-dir=node_modules \
  --exclude=release_check.sh \
  -e 'ghp_[A-Za-z0-9]{20,}' \
  -e 'github_pat_[A-Za-z0-9_]{20,}' \
  -e 'x-access-token:' \
  -e '_authToken=' \
  -e 'BEGIN [A-Z ]*PRIVATE KEY' \
  -e 'access_token[[:space:]]*[:=][[:space:]]*["'"'"'][A-Za-z0-9_.-]{16,}' \
  . 2>/dev/null | grep -v 'secrets\.GITHUB_TOKEN' || true)
if [ -z "${secret_hits}" ]; then
  ok "no hardcoded secret material detected"
else
  echo "${secret_hits}"
  bad "possible hardcoded secret detected (see lines above)"
fi

# --- 5. simulation boundary annotations still present ------------------
sim_count=$(grep -rIl --include='*.go' -i 'simulated' internal | wc -l | tr -d ' ')
if [ "${sim_count}" -gt 0 ]; then
  ok "simulation boundary annotations present (${sim_count} Go files under internal/)"
else
  bad "no 'simulated' annotations under internal/ — GRAM/TLM/hardware/MicroVM boundary lost"
fi

echo "----------------------------------------------------------------"
if [ "${fail}" -eq 0 ]; then
  echo "RELEASE CHECK OK"
else
  echo "RELEASE CHECK FAILED (${fail} failing check group(s))"
  exit 1
fi
