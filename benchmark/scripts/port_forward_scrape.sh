#!/usr/bin/env bash
# port_forward_scrape.sh — Supervised kubectl port-forward for Prometheus scrape target.
#
# Keeps the port-forward alive by restarting it automatically on exit so that
# Prometheus can continuously scrape the controller debug port even if the
# connection drops.
#
# Usage:
#   port_forward_scrape.sh [-h]
#
# Environment variables (all optional):
#   NAMESPACE    Kubernetes namespace   (default: prequal-benchmark)
#   SVC          Service name          (default: prequal-proxy)
#   PROXY_PORT   Local proxy port      (default: 31080)
#   DEBUG_PORT   Local debug/metrics port (default: 31081)
#
# Start in the background:
#   nohup benchmark/scripts/port_forward_scrape.sh &>/tmp/prequal-port-forward.log &
#
# Stop:
#   kill "$(cat /tmp/prequal-port-forward.pid)"

set -euo pipefail

usage() {
  grep '^#' "$0" | sed 's/^# \{0,1\}//'
  exit 1
}

[[ "${1:-}" == "-h" ]] && usage

NAMESPACE="${NAMESPACE:-prequal-benchmark}"
SVC="${SVC:-prequal-proxy}"
PROXY_PORT="${PROXY_PORT:-31080}"
DEBUG_PORT="${DEBUG_PORT:-31081}"

PID_FILE="/tmp/prequal-port-forward.pid"
BACKOFF_SEC=3

# Write our own PID so callers can kill the supervisor loop cleanly.
echo "$$" > "${PID_FILE}"

_cleanup() {
  echo "[port-forward] $(date -u +%Y-%m-%dT%H:%M:%SZ) caught signal; stopping." >&2
  # Kill the child port-forward if it is still running.
  if [[ -n "${CHILD_PID:-}" ]] && kill -0 "${CHILD_PID}" 2>/dev/null; then
    kill "${CHILD_PID}" 2>/dev/null || true
  fi
  rm -f "${PID_FILE}"
  exit 0
}

trap '_cleanup' INT TERM

echo "[port-forward] $(date -u +%Y-%m-%dT%H:%M:%SZ) starting supervisor" \
     "(ns=${NAMESPACE} svc=${SVC} proxy=${PROXY_PORT} debug=${DEBUG_PORT})" >&2

STOPPING=0
trap 'STOPPING=1; _cleanup' INT TERM

while true; do
  echo "[port-forward] $(date -u +%Y-%m-%dT%H:%M:%SZ) launching kubectl port-forward" >&2
  kubectl port-forward \
    --namespace "${NAMESPACE}" \
    --address 0.0.0.0 \
    "svc/${SVC}" \
    "${PROXY_PORT}:8080" \
    "${DEBUG_PORT}:8081" &
  CHILD_PID=$!

  # Wait for child; capture exit status without triggering set -e.
  wait "${CHILD_PID}" || true

  [[ "${STOPPING}" == "1" ]] && break

  echo "[port-forward] $(date -u +%Y-%m-%dT%H:%M:%SZ) port-forward exited; reconnecting in ${BACKOFF_SEC}s" >&2
  sleep "${BACKOFF_SEC}"
done
