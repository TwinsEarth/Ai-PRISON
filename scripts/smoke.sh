#!/usr/bin/env bash
#
# smoke.sh — end-to-end smoke test of the REAL compiled bin/aiprison binary.
#
# Zero residue by construction: every data directory is created under mktemp -d
# and removed by an EXIT trap; the script must be safe to re-run and must never
# write into the repo working tree. Invoked from the repo root via `make smoke`.
set -euo pipefail

# Resolve the repo root (this script lives in scripts/) so the smoke works no
# matter which directory make is started from.
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "${HERE}/.." && pwd)"
BIN="${ROOT}/bin/aiprison"

if [[ ! -x "${BIN}" ]]; then
    echo "smoke: ${BIN} not found; run 'make build' first" >&2
    exit 1
fi

# Two throwaway data directories; cleaned on EXIT in every path.
D1="$(mktemp -d -t aiprison-smoke-d1.XXXXXX)"
D2="$(mktemp -d -t aiprison-smoke-d2.XXXXXX)"
cleanup() { rm -rf "${D1}" "${D2}"; }
trap cleanup EXIT

echo "== smoke: binary =="
"${BIN}" version --verbose

echo "== smoke: demo into D1 (process 1) =="
DEMO_OUT="$("${BIN}" demo --data-dir "${D1}" 2>&1)" || {
    echo "demo failed:" >&2; echo "${DEMO_OUT}" >&2; exit 1;
}
echo "${DEMO_OUT}" | grep -q "已落盘记录数"

echo "== smoke: SAME binary, inspect D1 in a new process =="
INSPECT_OUT="$("${BIN}" inspect --data-dir "${D1}")"
echo "${INSPECT_OUT}" | grep -q "CHAIN VERIFY:    OK" || {
    echo "inspect: missing 'CHAIN VERIFY: OK':" >&2; echo "${INSPECT_OUT}" >&2; exit 1;
}

echo "== smoke: SAME binary, verify D1 in a new process =="
VERIFY_OUT="$("${BIN}" verify --data-dir "${D1}")"
echo "${VERIFY_OUT}" | grep -q "INTEGRITY OK" || {
    echo "verify: missing 'INTEGRITY OK':" >&2; echo "${VERIFY_OUT}" >&2; exit 1;
}
echo "${VERIFY_OUT}"

echo "== smoke: drill into D2 (process 1) =="
DRILL_OUT="$("${BIN}" drill --data-dir "${D2}" 2>&1)" || {
    echo "drill failed:" >&2; echo "${DRILL_OUT}" >&2; exit 1;
}
echo "${DRILL_OUT}" | grep -q "OVERALL: PASS"

echo "== smoke: verify D2 =="
"${BIN}" verify --data-dir "${D2}" | grep -q "INTEGRITY OK"

echo "== smoke: no leftover temp segment files =="
if compgen -G "${D1}"/.seg-*.tmp >/dev/null || compgen -G "${D2}"/.seg-*.tmp >/dev/null; then
    echo "smoke: leftover *.tmp segment files found" >&2
    ls -la "${D1}" "${D2}" >&2
    exit 1
fi

echo "== smoke: help exits 0 and unknown command exits non-zero =="
"${BIN}" help >/dev/null
set +e
"${BIN}" definitely-not-a-command --data-dir "${D1}" >/dev/null 2>&1
UNKNOWN_RC=$?
set -e
if [[ "${UNKNOWN_RC}" -eq 0 ]]; then
    echo "smoke: unknown command must exit non-zero, got 0" >&2
    exit 1
fi

echo "SMOKE OK"
