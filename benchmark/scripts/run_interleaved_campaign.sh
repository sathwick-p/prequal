#!/usr/bin/env bash
# run_interleaved_campaign.sh — Run multiple algorithms in interleaved order.
#
# Usage:
#   run_interleaved_campaign.sh [-h]
#
# Required env vars:
#   SCENARIO    Scenario name (e.g. heterogeneous-open-loop)
#   K6_SCRIPT   Path to a k6 .js script (e.g. benchmark/k6/open_loop.js)
#
# Optional env vars:
#   ALGORITHMS          Space-separated algorithm list (default: "prequal round-robin least-connections")
#   REPS                Number of repetitions per algorithm (default: 5)
#   INGRESS_NAME        Ingress resource name to patch (default: bench-heterogeneous)
#   NAMESPACE           Kubernetes namespace (default: prequal-benchmark)
#   TARGET_URL          Passed through to run_campaign.sh
#   HOST_HEADER         Passed through to run_campaign.sh
#   RATE                Passed through to run_campaign.sh
#   DURATION            Passed through to run_campaign.sh
#   WORK_ITERATIONS     Passed through to run_campaign.sh
#   WORKLOAD_MANIFEST   Passed through to run_campaign.sh
#   NOTES               Base notes string (algo/rep info appended automatically)
#   RESET_CONTROLLER    Passed through to run_campaign.sh (default: 0)
#   POOL_RESET_WARMUP_SEC Passed through to run_campaign.sh (default: 15)
#
# Outer loop: rep in 1..REPS; inner loop: algorithms.
# Restores lb/algo=prequal on the Ingress when done.

set -euo pipefail

usage() {
  grep '^#' "$0" | sed 's/^# \{0,1\}//'
  exit 1
}

[[ "${1:-}" == "-h" ]] && usage

# Validate required vars
missing=()
[[ -z "${SCENARIO:-}" ]]  && missing+=("SCENARIO")
[[ -z "${K6_SCRIPT:-}" ]] && missing+=("K6_SCRIPT")
if [[ ${#missing[@]} -gt 0 ]]; then
  echo "ERROR: missing required env vars: ${missing[*]}" >&2
  usage
fi

# Defaults
ALGORITHMS="${ALGORITHMS:-prequal round-robin least-connections}"
REPS="${REPS:-5}"
INGRESS_NAME="${INGRESS_NAME:-bench-heterogeneous}"
NAMESPACE="${NAMESPACE:-prequal-benchmark}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RUN_CAMPAIGN="${SCRIPT_DIR}/run_campaign.sh"

if [[ ! -x "${RUN_CAMPAIGN}" ]]; then
  echo "ERROR: run_campaign.sh not found or not executable at ${RUN_CAMPAIGN}" >&2
  exit 1
fi

# Build algorithm array
read -r -a ALGO_ARRAY <<< "${ALGORITHMS}"

echo "[interleaved] SCENARIO=${SCENARIO} REPS=${REPS} ALGORITHMS=${ALGORITHMS}"
echo "[interleaved] INGRESS_NAME=${INGRESS_NAME} NAMESPACE=${NAMESPACE}"

# Map algorithm names to ingress annotation values
_algo_annotation() {
  local algo="$1"
  case "${algo}" in
    prequal)            echo "prequal" ;;
    round-robin)        echo "round-robin" ;;
    least-connections)  echo "least-connections" ;;
    *)                  echo "${algo}" ;;
  esac
}

_patch_ingress() {
  local algo="$1"
  local annotation_val
  annotation_val="$(_algo_annotation "${algo}")"
  if command -v kubectl >/dev/null 2>&1; then
    echo "[interleaved] patching Ingress ${INGRESS_NAME} lb/algo=${annotation_val}..."
    kubectl -n "${NAMESPACE}" annotate ingress "${INGRESS_NAME}" \
      "lb/algo=${annotation_val}" --overwrite
    sleep 3
  else
    echo "[interleaved] WARNING: kubectl not found; cannot patch Ingress annotation" >&2
  fi
}

# Restore prequal on exit
_restore_ingress() {
  echo "[interleaved] restoring Ingress lb/algo=prequal..."
  if command -v kubectl >/dev/null 2>&1; then
    kubectl -n "${NAMESPACE}" annotate ingress "${INGRESS_NAME}" \
      "lb/algo=prequal" --overwrite 2>/dev/null || true
  fi
}
trap _restore_ingress EXIT

TOTAL_RUNS=$(( REPS * ${#ALGO_ARRAY[@]} ))
RUN_NUM=0

for rep in $(seq 1 "${REPS}"); do
  for algo in "${ALGO_ARRAY[@]}"; do
    RUN_NUM=$(( RUN_NUM + 1 ))
    echo ""
    echo "[interleaved] === run ${RUN_NUM}/${TOTAL_RUNS}: rep=${rep}/${REPS} algo=${algo} ==="

    _patch_ingress "${algo}"

    # Build notes string
    BASE_NOTES="${NOTES:-}"
    RUN_NOTES="rep ${rep}/${REPS} algo=${algo} INTERLEAVED"
    if [[ -n "${BASE_NOTES}" ]]; then
      RUN_NOTES="${BASE_NOTES}; ${RUN_NOTES}"
    fi

    SCENARIO="${SCENARIO}" \
    ALGORITHM="${algo}" \
    K6_SCRIPT="${K6_SCRIPT}" \
    NOTES="${RUN_NOTES}" \
    NAMESPACE="${NAMESPACE}" \
    RESET_CONTROLLER="${RESET_CONTROLLER:-0}" \
    POOL_RESET_WARMUP_SEC="${POOL_RESET_WARMUP_SEC:-15}" \
    TARGET_URL="${TARGET_URL:-}" \
    HOST_HEADER="${HOST_HEADER:-}" \
    RATE="${RATE:-}" \
    DURATION="${DURATION:-}" \
    WORK_ITERATIONS="${WORK_ITERATIONS:-}" \
    WORKLOAD_MANIFEST="${WORKLOAD_MANIFEST:-}" \
    "${RUN_CAMPAIGN}"
  done
done

echo ""
echo "[interleaved] all ${TOTAL_RUNS} runs complete."
