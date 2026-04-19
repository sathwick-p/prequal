#!/usr/bin/env bash
# render_report.sh — Render a Markdown run report from run-metadata.json and k6-summary.json.
#
# Usage:
#   render_report.sh <run-directory>
#   render_report.sh -h
#
# Reads:
#   <run-dir>/run-metadata.json
#   <run-dir>/k6-summary.json
#   benchmark/report/templates/run-report.md
#
# Writes:
#   <run-dir>/report.md
#
# Token whitelist substituted from run-metadata.json and k6-summary.json:
#   ${RUN_ID}            — run_id
#   ${DATE_UTC}          — date_utc
#   ${GIT_SHA}           — git_sha
#   ${ENVIRONMENT}       — environment
#   ${SCENARIO}          — scenario
#   ${ALGORITHM}         — algorithm
#   ${LOAD_SCRIPT}       — load_script
#   ${WORKLOAD_MANIFEST} — workload_manifest
#   ${TARGET_RATE_RPS}   — target_rate_rps
#   ${DURATION_SECONDS}  — duration_seconds
#   ${NOTES}             — notes
#   ${TOTAL_REQUESTS}    — from k6 metrics: http_reqs.values.count
#   ${SUCCESS_RATE}      — derived: (1 - http_req_failed rate) * 100
#   ${P50_MS}            — http_req_duration.values.p(50) in ms
#   ${P95_MS}            — http_req_duration.values.p(95) in ms
#   ${P99_MS}            — http_req_duration.values.p(99) in ms
#   ${ERROR_RATE}        — http_req_failed.values.rate * 100
#
# Requires jq if available; falls back to grep/sed with reduced accuracy.

set -euo pipefail

usage() {
  grep '^#' "$0" | sed 's/^# \{0,1\}//'
  exit 1
}

[[ "${1:-}" == "-h" ]] && usage

if [[ $# -ne 1 ]]; then
  echo "ERROR: expected exactly one argument (run directory)" >&2
  usage
fi

OUT="${1%/}"
NOTES_FILE="${OUT}/notes.md"

if [[ ! -d "${OUT}" ]]; then
  echo "ERROR: directory does not exist: ${OUT}" >&2
  exit 1
fi

METADATA="${OUT}/run-metadata.json"
SUMMARY="${OUT}/k6-summary.json"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEMPLATE="${SCRIPT_DIR}/../report/templates/run-report.md"

for f in "${METADATA}" "${SUMMARY}" "${TEMPLATE}"; do
  if [[ ! -f "${f}" ]]; then
    echo "ERROR: required file not found: ${f}" >&2
    exit 1
  fi
done

note() {
  echo "$*" >> "${NOTES_FILE}"
}

TIMESTAMP="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

if command -v jq >/dev/null 2>&1; then
  # ── jq path ────────────────────────────────────────────────────────────────
  RUN_ID="$(          jq -r '.run_id'            "${METADATA}")"
  DATE_UTC="$(        jq -r '.date_utc'          "${METADATA}")"
  GIT_SHA="$(         jq -r '.git_sha'           "${METADATA}")"
  ENVIRONMENT="$(     jq -r '.environment'       "${METADATA}")"
  SCENARIO="$(        jq -r '.scenario'          "${METADATA}")"
  ALGORITHM="$(       jq -r '.algorithm'         "${METADATA}")"
  LOAD_SCRIPT="$(     jq -r '.load_script'       "${METADATA}")"
  WORKLOAD_MANIFEST="$(jq -r '.workload_manifest' "${METADATA}")"
  TARGET_RATE_RPS="$( jq -r '.target_rate_rps'   "${METADATA}")"
  DURATION_SECONDS="$(jq -r '.duration_seconds'  "${METADATA}")"
  NOTES="$(           jq -r '.notes'             "${METADATA}")"

  TOTAL_REQUESTS="$(  jq -r '.metrics.http_reqs.values.count          // "n/a"' "${SUMMARY}")"
  ERROR_RATE_RAW="$(  jq -r '.metrics.http_req_failed.values.rate     // 0'     "${SUMMARY}")"
  P50_MS="$(          jq -r '.metrics.http_req_duration.values["p(50)"] // "n/a"' "${SUMMARY}")"
  P95_MS="$(          jq -r '.metrics.http_req_duration.values["p(95)"] // "n/a"' "${SUMMARY}")"
  P99_MS="$(          jq -r '.metrics.http_req_duration.values["p(99)"] // "n/a"' "${SUMMARY}")"

  # Compute SUCCESS_RATE and ERROR_RATE as percentages (handle non-numeric gracefully)
  if [[ "${ERROR_RATE_RAW}" =~ ^[0-9.]+$ ]]; then
    ERROR_RATE="$(awk "BEGIN{printf \"%.2f\", ${ERROR_RATE_RAW} * 100}")"
    SUCCESS_RATE="$(awk "BEGIN{printf \"%.2f\", (1 - ${ERROR_RATE_RAW}) * 100}")"
  else
    ERROR_RATE="n/a"
    SUCCESS_RATE="n/a"
  fi

else
  # ── grep/sed fallback ──────────────────────────────────────────────────────
  note ""
  note "## render_report.sh warning (${TIMESTAMP})"
  note "- jq not found; fell back to grep/sed extraction. Metric values may be inaccurate."
  note ""

  _extract() { grep -o "\"${1}\"[[:space:]]*:[[:space:]]*\"[^\"]*\"" "$2" | head -1 | sed 's/.*: *"\(.*\)"/\1/'; }
  _extract_num() { grep -o "\"${1}\"[[:space:]]*:[[:space:]]*[0-9.]*" "$2" | head -1 | sed 's/.*: *//'; }

  RUN_ID="$(          _extract run_id            "${METADATA}")"
  DATE_UTC="$(        _extract date_utc          "${METADATA}")"
  GIT_SHA="$(         _extract git_sha           "${METADATA}")"
  ENVIRONMENT="$(     _extract environment       "${METADATA}")"
  SCENARIO="$(        _extract scenario          "${METADATA}")"
  ALGORITHM="$(       _extract algorithm         "${METADATA}")"
  LOAD_SCRIPT="$(     _extract load_script       "${METADATA}")"
  WORKLOAD_MANIFEST="$(_extract workload_manifest "${METADATA}")"
  TARGET_RATE_RPS="$( _extract_num target_rate_rps "${METADATA}")"
  DURATION_SECONDS="$(_extract_num duration_seconds "${METADATA}")"
  NOTES="$(           _extract notes             "${METADATA}")"

  TOTAL_REQUESTS="n/a (jq required)"
  SUCCESS_RATE="n/a (jq required)"
  P50_MS="n/a (jq required)"
  P95_MS="n/a (jq required)"
  P99_MS="n/a (jq required)"
  ERROR_RATE="n/a (jq required)"
fi

export RUN_ID DATE_UTC GIT_SHA ENVIRONMENT SCENARIO ALGORITHM LOAD_SCRIPT \
       WORKLOAD_MANIFEST TARGET_RATE_RPS DURATION_SECONDS NOTES \
       TOTAL_REQUESTS SUCCESS_RATE P50_MS P95_MS P99_MS ERROR_RATE

envsubst '${RUN_ID} ${DATE_UTC} ${GIT_SHA} ${ENVIRONMENT} ${SCENARIO} ${ALGORITHM} ${LOAD_SCRIPT} ${WORKLOAD_MANIFEST} ${TARGET_RATE_RPS} ${DURATION_SECONDS} ${NOTES} ${TOTAL_REQUESTS} ${SUCCESS_RATE} ${P50_MS} ${P95_MS} ${P99_MS} ${ERROR_RATE}' \
  < "${TEMPLATE}" > "${OUT}/report.md"

echo "[render_report] wrote ${OUT}/report.md"
