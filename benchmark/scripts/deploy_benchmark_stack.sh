#!/usr/bin/env bash
# deploy_benchmark_stack.sh — Apply benchmark manifests and wait for controller rollout.
#
# Usage:
#   deploy_benchmark_stack.sh [-h]
#
# Env vars:
#   NAMESPACE      Kubernetes namespace  (default: prequal-benchmark)
#   MANIFESTS_DIR  Path to manifests dir (default: benchmark/manifests)
#   WORKLOAD       Workload manifest filename under MANIFESTS_DIR
#                  (default: workload-uniform.yaml)
#
# Idempotent: calling twice does not fail.

set -euo pipefail

usage() {
  grep '^#' "$0" | sed 's/^# \{0,1\}//' | head -20
  exit 1
}

[[ "${1:-}" == "-h" ]] && usage

NAMESPACE="${NAMESPACE:-prequal-benchmark}"
MANIFESTS_DIR="${MANIFESTS_DIR:-benchmark/manifests}"
WORKLOAD="${WORKLOAD:-workload-uniform.yaml}"

echo "[deploy] namespace=${NAMESPACE} manifests=${MANIFESTS_DIR} workload=${WORKLOAD}"

kubectl apply -f "${MANIFESTS_DIR}/controller-benchmark.yaml"
kubectl apply -f "${MANIFESTS_DIR}/${WORKLOAD}"

echo "[deploy] waiting for controller rollout (timeout 180s)..."
kubectl -n "${NAMESPACE}" rollout status deployment/prequal-controller --timeout=180s

echo "[deploy] done."
