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
#   RESET_CONTROLLER        Set to 1 to rollout-restart prequal-controller between runs (default: 0)
#   POOL_RESET_WARMUP_SEC   Seconds to wait after controller restart before k6 traffic (default: 15)
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
RESET_CONTROLLER="${RESET_CONTROLLER:-0}"
POOL_RESET_WARMUP_SEC="${POOL_RESET_WARMUP_SEC:-15}"
PROMETHEUS_URL="${PROMETHEUS_URL:-http://localhost:9090}"

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

# ── Capture controller / backend env vars ─────────────────────────────────────
# Best-effort; falls back to [] when kubectl is unavailable or pods are absent.
_capture_controller_env() {
  local namespace="$1"
  if ! command -v kubectl >/dev/null 2>&1; then
    echo '[]'
    return
  fi
  local raw
  raw="$(kubectl -n "${namespace}" get pod \
    -l app=prequal-controller \
    -o jsonpath='{.items[0].spec.containers[0].env}' 2>/dev/null || echo '[]')"
  [[ -z "${raw}" ]] && raw='[]'
  if command -v jq >/dev/null 2>&1; then
    echo "${raw}" | jq '[.[] | select(.name | startswith("PREQUAL_"))]' 2>/dev/null || echo '[]'
  else
    echo "${raw}"
  fi
}

_capture_backend_env() {
  local namespace="$1"
  if ! command -v kubectl >/dev/null 2>&1; then
    echo '[]'
    return
  fi
  local raw='[]'
  # Try heterogeneous label first, then uniform label
  for selector in \
    'app.kubernetes.io/part-of=bench-heterogeneous' \
    'app=bench-uniform'; do
    local candidate
    candidate="$(kubectl -n "${namespace}" get pod \
      -l "${selector}" \
      -o jsonpath='{.items[0].spec.containers[0].env}' 2>/dev/null || true)"
    if [[ -n "${candidate}" && "${candidate}" != "null" ]]; then
      raw="${candidate}"
      break
    fi
  done
  if command -v jq >/dev/null 2>&1; then
    echo "${raw}" | jq '[.[] | select(.name | startswith("WORK_") or startswith("FAULT_"))]' 2>/dev/null || echo '[]'
  else
    echo "${raw}"
  fi
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

# ── Capture env snapshots ─────────────────────────────────────────────────────
echo "[run_campaign] capturing controller_env and backend_env..."
CONTROLLER_ENV_JSON="$(_capture_controller_env "${NAMESPACE}")"
BACKEND_ENV_JSON="$(_capture_backend_env "${NAMESPACE}")"

JQ_AVAILABLE=0
command -v jq >/dev/null 2>&1 && JQ_AVAILABLE=1

# Write run-metadata.json
DURATION_SECONDS="${DURATION:-0}"
# Strip trailing 's' if present for the numeric field
DURATION_SECONDS="${DURATION_SECONDS%s}"

TARGET_RATE_RPS="${RATE:-0}"

NOTES_FILE="${OUT}/notes.md"

if [[ "${JQ_AVAILABLE}" == "1" ]]; then
  jq -n \
    --arg run_id "${DATE_UTC}-${SCENARIO}-${ALGORITHM}" \
    --arg date_utc "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    --arg git_sha "${GIT_SHA}" \
    --arg environment "${ENVIRONMENT:-local}" \
    --arg scenario "${SCENARIO}" \
    --arg algorithm "${ALGORITHM}" \
    --argjson duration_seconds "${DURATION_SECONDS}" \
    --argjson target_rate_rps "${TARGET_RATE_RPS}" \
    --arg workload_manifest "${WORKLOAD_MANIFEST}" \
    --arg load_script "${K6_SCRIPT}" \
    --argjson controller_env "${CONTROLLER_ENV_JSON}" \
    --argjson backend_env "${BACKEND_ENV_JSON}" \
    --arg notes "${NOTES}" \
    '{
      run_id: $run_id,
      date_utc: $date_utc,
      git_sha: $git_sha,
      environment: $environment,
      scenario: $scenario,
      algorithm: $algorithm,
      duration_seconds: $duration_seconds,
      target_rate_rps: $target_rate_rps,
      workload_manifest: $workload_manifest,
      load_script: $load_script,
      controller_env: $controller_env,
      backend_env: $backend_env,
      notes: $notes
    }' > "${OUT}/run-metadata.json"
else
  # jq unavailable: embed raw JSON strings and note degradation
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
  "controller_env": ${CONTROLLER_ENV_JSON},
  "backend_env": ${BACKEND_ENV_JSON},
  "notes": "${NOTES}"
}
EOF
  echo "- WARNING: jq not available; controller_env/backend_env written as raw JSON strings (may not be filtered)" \
    >> "${NOTES_FILE}"
fi

echo "[run_campaign] wrote run-metadata.json"

# ── Optional pool reset ───────────────────────────────────────────────────────
if [[ "${RESET_CONTROLLER}" == "1" ]]; then
  if command -v kubectl >/dev/null 2>&1; then
    echo "[run_campaign] RESET_CONTROLLER=1: restarting prequal-controller..."
    kubectl -n "${NAMESPACE}" rollout restart deployment/prequal-controller
    kubectl -n "${NAMESPACE}" rollout status deployment/prequal-controller --timeout=120s
    echo "[run_campaign] controller ready; sleeping ${POOL_RESET_WARMUP_SEC}s for pool warm-up..."
    sleep "${POOL_RESET_WARMUP_SEC}"
    echo "- pool reset: rollout-restarted prequal-controller; warmup=${POOL_RESET_WARMUP_SEC}s before k6 traffic ($(date -u +%Y-%m-%dT%H:%M:%SZ))" \
      >> "${NOTES_FILE}"
  else
    echo "[run_campaign] WARNING: RESET_CONTROLLER=1 but kubectl not found; skipping reset" >&2
    echo "- pool reset: SKIPPED (kubectl not found)" >> "${NOTES_FILE}"
  fi
fi

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

# ── Random-fallback rate snapshot ─────────────────────────────────────────────
# Best-effort Prometheus instant query for random_fallback rate over the run window.
mkdir -p "${OUT}/prometheus-export"
PROM_REACHABLE=0
if command -v curl >/dev/null 2>&1; then
  if curl -sf --max-time 5 "${PROMETHEUS_URL}/-/ready" >/dev/null 2>&1; then
    PROM_REACHABLE=1
  fi
fi

if [[ "${PROM_REACHABLE}" == "1" ]]; then
  DURATION_SECONDS_QUERY="${DURATION_SECONDS:-300}"
  [[ "${DURATION_SECONDS_QUERY}" == "0" ]] && DURATION_SECONDS_QUERY=300
  RF_OUT="${OUT}/prometheus-export/random_fallback_rate.json"
  if curl -sf --max-time 15 \
      "http://localhost:9090/api/v1/query?query=sum(rate(prequal_selection_algorithm_total%7Balgorithm%3D%22random_fallback%22%7D%5B${DURATION_SECONDS_QUERY}s%5D))" \
      > "${RF_OUT}" 2>/dev/null; then
    echo "[run_campaign] wrote prometheus-export/random_fallback_rate.json"
    echo "- random_fallback_rate.json: collected from Prometheus" >> "${NOTES_FILE}"
  else
    echo "[run_campaign] WARNING: random_fallback_rate query failed; skipping" >&2
    echo "- random_fallback_rate.json: SKIPPED (Prometheus query failed)" >> "${NOTES_FILE}"
  fi
else
  echo "[run_campaign] WARNING: Prometheus not reachable; skipping random_fallback_rate snapshot" >&2
  echo "- random_fallback_rate.json: SKIPPED (Prometheus not reachable at ${PROMETHEUS_URL})" >> "${NOTES_FILE}"
fi

echo "[run_campaign] done. results at: ${OUT}"
