#!/usr/bin/env bash
# collect_results.sh — Best-effort collection of artifacts for a single run directory.
#
# Usage:
#   collect_results.sh <output-directory>
#   collect_results.sh -h
#
# Collects (skips gracefully if tool missing or endpoint unreachable):
#   kubectl-top.txt          — kubectl top pods -n prequal-benchmark
#   routes.json              — GET http://127.0.0.1:31081/routes
#   controller-logs.txt      — last 2000 lines from deploy/prequal-controller
#   backend-logs.txt         — concatenated logs from bench-* deployments
#   prometheus-export/*.json — small PromQL snapshot set (if Prometheus reachable)
#   notes.md                 — collection log; appended on every run

set -euo pipefail

usage() {
  grep '^#' "$0" | sed 's/^# \{0,1\}//'
  exit 1
}

[[ "${1:-}" == "-h" ]] && usage

if [[ $# -ne 1 ]]; then
  echo "ERROR: expected exactly one argument (output directory)" >&2
  usage
fi

OUT="$1"

if [[ ! -d "${OUT}" ]]; then
  echo "ERROR: output directory does not exist: ${OUT}" >&2
  exit 1
fi

NAMESPACE="${NAMESPACE:-prequal-benchmark}"
CONTROLLER_HOST="${CONTROLLER_HOST:-127.0.0.1}"
CONTROLLER_PORT="${CONTROLLER_PORT:-31081}"
PROMETHEUS_URL="${PROMETHEUS_URL:-http://localhost:9090}"
NOTES="${OUT}/notes.md"
TIMESTAMP="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

note() {
  echo "$*" >> "${NOTES}"
}

note ""
note "## Collection run: ${TIMESTAMP}"
note ""

echo "[collect] output dir: ${OUT}"

# ── kubectl top ───────────────────────────────────────────────────────────────
if command -v kubectl >/dev/null 2>&1; then
  echo "[collect] kubectl top pods..."
  if kubectl top pods -n "${NAMESPACE}" > "${OUT}/kubectl-top.txt" 2>&1; then
    note "- kubectl-top.txt: collected"
  else
    note "- kubectl-top.txt: kubectl top failed (metrics-server may not be available)"
  fi
else
  note "- kubectl-top.txt: SKIPPED (kubectl not found)"
fi

# ── /routes snapshot ──────────────────────────────────────────────────────────
if command -v curl >/dev/null 2>&1; then
  echo "[collect] GET /routes..."
  if curl -sS --max-time 10 \
      "http://${CONTROLLER_HOST}:${CONTROLLER_PORT}/routes" \
      -o "${OUT}/routes.json" 2>>"${NOTES}"; then
    note "- routes.json: collected from http://${CONTROLLER_HOST}:${CONTROLLER_PORT}/routes"
  else
    note "- routes.json: SKIPPED (controller /routes not reachable at ${CONTROLLER_HOST}:${CONTROLLER_PORT})"
  fi
else
  note "- routes.json: SKIPPED (curl not found)"
fi

# ── controller logs ───────────────────────────────────────────────────────────
if command -v kubectl >/dev/null 2>&1; then
  echo "[collect] controller logs..."
  if kubectl -n "${NAMESPACE}" logs deploy/prequal-controller --tail=2000 \
      > "${OUT}/controller-logs.txt" 2>&1; then
    note "- controller-logs.txt: collected (tail=2000)"
  else
    note "- controller-logs.txt: SKIPPED (kubectl logs failed)"
  fi
else
  note "- controller-logs.txt: SKIPPED (kubectl not found)"
fi

# ── backend logs ──────────────────────────────────────────────────────────────
if command -v kubectl >/dev/null 2>&1; then
  echo "[collect] backend logs..."
  BACKEND_DEPLOYMENTS="$(kubectl -n "${NAMESPACE}" get deployments \
    --no-headers -o custom-columns=NAME:.metadata.name 2>/dev/null \
    | grep '^bench-' || true)"
  if [[ -n "${BACKEND_DEPLOYMENTS}" ]]; then
    : > "${OUT}/backend-logs.txt"
    while IFS= read -r dep; do
      echo "===== ${dep} =====" >> "${OUT}/backend-logs.txt"
      kubectl -n "${NAMESPACE}" logs "deploy/${dep}" --tail=500 \
        >> "${OUT}/backend-logs.txt" 2>&1 || true
    done <<< "${BACKEND_DEPLOYMENTS}"
    note "- backend-logs.txt: collected from deployments: $(echo "${BACKEND_DEPLOYMENTS}" | tr '\n' ' ')"
  else
    note "- backend-logs.txt: SKIPPED (no bench-* deployments found in namespace ${NAMESPACE})"
  fi
else
  note "- backend-logs.txt: SKIPPED (kubectl not found)"
fi

# ── Prometheus snapshot ───────────────────────────────────────────────────────
PROM_API="${PROMETHEUS_URL}/api/v1/query"
PROM_RANGE_API="${PROMETHEUS_URL}/api/v1/query_range"
PROM_REACHABLE=0
if command -v curl >/dev/null 2>&1; then
  if curl -sS --max-time 5 "${PROMETHEUS_URL}/-/ready" >/dev/null 2>&1; then
    PROM_REACHABLE=1
  fi
fi

if [[ "${PROM_REACHABLE}" == "1" ]]; then
  echo "[collect] Prometheus snapshot..."
  mkdir -p "${OUT}/prometheus-export"

  prom_query() {
    local name="$1"
    local query="$2"
    local outfile="${OUT}/prometheus-export/${name}.json"
    if curl -sS --max-time 15 \
        --data-urlencode "query=${query}" \
        "${PROM_API}" \
        -o "${outfile}" 2>/dev/null; then
      note "- prometheus-export/${name}.json: collected"
    else
      note "- prometheus-export/${name}.json: SKIPPED (query failed)"
    fi
  }

  prom_query "request_rate" \
    'rate(prequal_proxy_requests_total[1m])'

  prom_query "p95_latency" \
    'histogram_quantile(0.95, rate(prequal_proxy_request_duration_seconds_bucket[1m]))'

  prom_query "probe_queue_depth" \
    'prequal_probe_queue_depth'

  prom_query "pool_occupancy" \
    'prequal_pool_occupancy'

  # ── Prometheus range queries (time-series evidence) ────────────────────────
  # Use RUN_START_UTC / RUN_END_UTC exported by run_campaign.sh when available;
  # otherwise fall back to a 5-minute window ending now.
  echo "[collect] Prometheus range queries..."
  RANGE_END="${RUN_END_UTC:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
  if [[ -n "${RUN_START_UTC:-}" ]]; then
    RANGE_START="${RUN_START_UTC}"
  else
    # Default: 5 minutes before end
    RANGE_START="$(date -u -d "${RANGE_END} - 300 seconds" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null \
      || date -u -v-300S +%Y-%m-%dT%H:%M:%SZ 2>/dev/null \
      || echo "${RANGE_END}")"
  fi

  prom_range_query() {
    local name="$1"
    local query="$2"
    local outfile="${OUT}/prometheus-export/${name}-range.json"
    if curl -sS --max-time 30 \
        --data-urlencode "query=${query}" \
        --data-urlencode "start=${RANGE_START}" \
        --data-urlencode "end=${RANGE_END}" \
        --data-urlencode "step=15s" \
        "${PROM_RANGE_API}" \
        -o "${outfile}" 2>/dev/null; then
      note "- prometheus-export/${name}-range.json: collected (${RANGE_START} to ${RANGE_END})"
    else
      note "- prometheus-export/${name}-range.json: SKIPPED (range query failed)"
    fi
  }

  prom_range_query "cpu_rate" \
    'rate(process_cpu_seconds_total{job="prequal-controller"}[1m])'

  prom_range_query "rss_bytes" \
    'process_resident_memory_bytes{job="prequal-controller"}'

  prom_range_query "p95_latency_by_route" \
    'histogram_quantile(0.95, sum by (le, route_key) (rate(prequal_proxy_request_duration_seconds_bucket[1m])))'

  prom_range_query "p99_latency_by_route" \
    'histogram_quantile(0.99, sum by (le, route_key) (rate(prequal_proxy_request_duration_seconds_bucket[1m])))'

else
  note "- prometheus-export/: SKIPPED (Prometheus not reachable at ${PROMETHEUS_URL})"
fi

note ""
note "Collection complete: ${TIMESTAMP}"

echo "[collect] done. notes at: ${NOTES}"
