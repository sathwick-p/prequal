#!/usr/bin/env bash
# run_campaign.sh — Orchestrate one full benchmark scenario run.
#
# Usage:
#   run_campaign.sh [-h]
#
# Required env vars:
#   SCENARIO    Scenario name (e.g. uniform-open-loop)
#   ALGORITHM   Load-balancing algorithm: prequal | round-robin | least-connections
#   K6_SCRIPT   Path to a k6 .js script (e.g. benchmark/k6/open_loop.js)
#
# Optional env vars:
#   TARGET_URL              URL sent to k6  (default: http://127.0.0.1:31080/work)
#   HOST_HEADER             Host header     (default: bench.local)
#   DURATION                k6 DURATION env var (passed through if set)
#   RATE                    k6 RATE env var     (passed through if set)
#   WORK_ITERATIONS         k6 WORK_ITERATIONS  (passed through if set)
#   WORKLOAD_MANIFEST       Path to workload manifest (default: benchmark/manifests/workload-uniform.yaml)
#   RESULTS_DIR             Root results directory    (default: benchmark/results)
#   NOTES                   Free-text annotation for this run (optional)
#   K6_DRY_RUN              Set to 1 to skip actual k6 invocation (for testing)
#   KUBECTL_TOP_INTERVAL_SEC Seconds between kubectl top samples (default: 5)
#   NAMESPACE               Kubernetes namespace for kubectl top (default: prequal-benchmark)
#
# Output: deterministic directory <RESULTS_DIR>/<DATE_UTC>-<SCENARIO>-<ALGORITHM>/
# Calls collect_results.sh on completion.

set -euo pipefail

usage() {
  grep '^#' "$0" | sed 's/^# \{0,1\}//'
  exit 1
}

[[ "${1:-}" == "-h" ]] && usage

# Validate required vars
missing=()
[[ -z "${SCENARIO:-}" ]]   && missing+=("SCENARIO")
[[ -z "${ALGORITHM:-}" ]]  && missing+=("ALGORITHM")
[[ -z "${K6_SCRIPT:-}" ]]  && missing+=("K6_SCRIPT")
if [[ ${#missing[@]} -gt 0 ]]; then
  echo "ERROR: missing required env vars: ${missing[*]}" >&2
  usage
fi

# Defaults
TARGET_URL="${TARGET_URL:-http://127.0.0.1:31080/work}"
HOST_HEADER="${HOST_HEADER:-bench.local}"
WORKLOAD_MANIFEST="${WORKLOAD_MANIFEST:-benchmark/manifests/workload-uniform.yaml}"
RESULTS_DIR="${RESULTS_DIR:-benchmark/results}"
NOTES="${NOTES:-}"
K6_DRY_RUN="${K6_DRY_RUN:-0}"
KUBECTL_TOP_INTERVAL_SEC="${KUBECTL_TOP_INTERVAL_SEC:-5}"
NAMESPACE="${NAMESPACE:-prequal-benchmark}"

# ── kubectl top sampler ────────────────────────────────────────────────────────
# Runs in the background while k6 is active; writes a TSV time-series to
# ${OUT}/kubectl-top-timeseries.tsv with columns: TIMESTAMP TAB POD TAB CPU TAB MEM.
# Skips silently if kubectl is not present or top returns an error.
_sample_kubectl_top() {
  local outfile="$1"
  local interval="$2"
  local namespace="$3"
  # Write header
  printf 'TIMESTAMP\tPOD\tCPU\tMEM\n' > "${outfile}"
  if ! command -v kubectl >/dev/null 2>&1; then
    return
  fi
  while true; do
    local ts
    ts="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    kubectl top pods -n "${namespace}" --no-headers 2>/dev/null \
      | while IFS= read -r line; do
          local pod cpu mem
          pod="$(echo "${line}" | awk '{print $1}')"
          cpu="$(echo "${line}" | awk '{print $2}')"
          mem="$(echo "${line}" | awk '{print $3}')"
          printf '%s\t%s\t%s\t%s\n' "${ts}" "${pod}" "${cpu}" "${mem}"
        done >> "${outfile}" || true
    sleep "${interval}"
  done
}

# Resolve git SHA gracefully
GIT_SHA="unknown"
if command -v git >/dev/null 2>&1; then
  GIT_SHA="$(git rev-parse HEAD 2>/dev/null || echo unknown)"
fi

DATE_UTC="$(date -u +%Y-%m-%dT%H-%M-%SZ)"
OUT="${RESULTS_DIR}/${DATE_UTC}-${SCENARIO}-${ALGORITHM}"

mkdir -p "${OUT}"

echo "[run_campaign] output dir: ${OUT}"

# Write run-metadata.json
DURATION_SECONDS="${DURATION:-0}"
# Strip trailing 's' if present for the numeric field
DURATION_SECONDS="${DURATION_SECONDS%s}"

TARGET_RATE_RPS="${RATE:-0}"

cat > "${OUT}/run-metadata.json" <<EOF
{
  "run_id": "${DATE_UTC}-${SCENARIO}-${ALGORITHM}",
  "date_utc": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "git_sha": "${GIT_SHA}",
  "environment": "${ENVIRONMENT:-local}",
  "scenario": "${SCENARIO}",
  "algorithm": "${ALGORITHM}",
  "duration_seconds": ${DURATION_SECONDS},
  "target_rate_rps": ${TARGET_RATE_RPS},
  "workload_manifest": "${WORKLOAD_MANIFEST}",
  "load_script": "${K6_SCRIPT}",
  "controller_env": {},
  "backend_env": {},
  "notes": "${NOTES}"
}
EOF

echo "[run_campaign] wrote run-metadata.json"

# Build k6 command
K6_ARGS=(
  run
  "--summary-export=${OUT}/k6-summary.json"
  -e "TARGET_URL=${TARGET_URL}"
  -e "HOST_HEADER=${HOST_HEADER}"
)

# Forward optional k6 env vars if set
[[ -n "${DURATION:-}"        ]] && K6_ARGS+=(-e "DURATION=${DURATION}")
[[ -n "${RATE:-}"            ]] && K6_ARGS+=(-e "RATE=${RATE}")
[[ -n "${WORK_ITERATIONS:-}" ]] && K6_ARGS+=(-e "WORK_ITERATIONS=${WORK_ITERATIONS}")

K6_ARGS+=("${K6_SCRIPT}")

K6_CMD="k6 ${K6_ARGS[*]}"
echo "${K6_CMD}" > "${OUT}/command.txt"
echo "[run_campaign] command: ${K6_CMD}"

# Start background kubectl-top sampler
SAMPLER_PID=""
TIMESERIES_FILE="${OUT}/kubectl-top-timeseries.tsv"
_sample_kubectl_top "${TIMESERIES_FILE}" "${KUBECTL_TOP_INTERVAL_SEC}" "${NAMESPACE}" &
SAMPLER_PID=$!
echo "[run_campaign] started kubectl-top sampler (pid=${SAMPLER_PID}, interval=${KUBECTL_TOP_INTERVAL_SEC}s)"

# Export timestamps for collect_results.sh range queries
export RUN_START_UTC
RUN_START_UTC="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

if [[ "${K6_DRY_RUN}" == "1" ]]; then
  echo "[run_campaign] K6_DRY_RUN=1: skipping k6 invocation"
  # Write a minimal stub summary so downstream scripts don't break
  cat > "${OUT}/k6-summary.json" <<'ENDJSON'
{"metrics":{},"root_group":{},"_dry_run":true}
ENDJSON
else
  k6 "${K6_ARGS[@]}"
fi

export RUN_END_UTC
RUN_END_UTC="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

# Stop the background sampler
if [[ -n "${SAMPLER_PID}" ]] && kill -0 "${SAMPLER_PID}" 2>/dev/null; then
  kill "${SAMPLER_PID}" 2>/dev/null || true
  wait "${SAMPLER_PID}" 2>/dev/null || true
  echo "[run_campaign] stopped kubectl-top sampler (pid=${SAMPLER_PID})"
fi

echo "[run_campaign] k6 finished"

# Collect results
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COLLECT="${SCRIPT_DIR}/collect_results.sh"
if [[ -x "${COLLECT}" ]]; then
  echo "[run_campaign] calling collect_results.sh..."
  "${COLLECT}" "${OUT}"
else
  echo "[run_campaign] WARNING: ${COLLECT} not found or not executable; skipping collection" >&2
fi

echo "[run_campaign] done. results at: ${OUT}"
