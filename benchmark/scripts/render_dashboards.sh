#!/usr/bin/env bash
# render_dashboards.sh — Export Grafana dashboards as PNGs for offline reports.
#
# Usage:
#   render_dashboards.sh <output-dir> [from-ms] [to-ms]
#
# Required arg:
#   output-dir      Destination directory for <uid>.png files.
#
# Optional args:
#   from-ms         Grafana time range start (epoch ms or Grafana "now-1h").
#                   Default: now-1h
#   to-ms           Grafana time range end (epoch ms or Grafana "now").
#                   Default: now
#
# Env vars:
#   GRAFANA_URL     Default: http://localhost:3000
#   GRAFANA_USER    Default: admin
#   GRAFANA_PASS    Default: admin
#   WIDTH           Default: 1600
#   HEIGHT          Default: 1000
#   TZ_NAME         Default: UTC
#
# Relies on the grafana-image-renderer sidecar wired via docker-compose.

set -euo pipefail

usage() {
  grep '^#' "$0" | sed 's/^# \{0,1\}//'
  exit 1
}

[[ "${1:-}" == "-h" || $# -lt 1 ]] && usage

OUT="$1"
FROM="${2:-now-1h}"
TO="${3:-now}"

GRAFANA_URL="${GRAFANA_URL:-http://localhost:3000}"
GRAFANA_USER="${GRAFANA_USER:-admin}"
GRAFANA_PASS="${GRAFANA_PASS:-admin}"
WIDTH="${WIDTH:-1600}"
HEIGHT="${HEIGHT:-1000}"
TZ_NAME="${TZ_NAME:-UTC}"

mkdir -p "${OUT}"

echo "[render] grafana=${GRAFANA_URL} out=${OUT} from=${FROM} to=${TO}"

DASHBOARDS="$(curl -sf -u "${GRAFANA_USER}:${GRAFANA_PASS}" \
  "${GRAFANA_URL}/api/search?type=dash-db" \
  | jq -r '.[] | "\(.uid)|\(.url)"')"

if [[ -z "${DASHBOARDS}" ]]; then
  echo "[render] ERROR: no dashboards returned from Grafana API" >&2
  exit 1
fi

ok=0
fail=0
while IFS='|' read -r uid url; do
  [[ -z "${uid}" ]] && continue
  # url format: /d/<uid>/<slug>
  png="${OUT}/${uid}.png"
  render_url="${GRAFANA_URL}/render${url}?orgId=1&from=${FROM}&to=${TO}&width=${WIDTH}&height=${HEIGHT}&tz=${TZ_NAME}&kiosk=1"
  echo "[render] ${uid} -> ${png}"
  if curl -sf -u "${GRAFANA_USER}:${GRAFANA_PASS}" -o "${png}" "${render_url}"; then
    bytes=$(wc -c < "${png}" | tr -d ' ')
    if [[ "${bytes}" -lt 2048 ]]; then
      echo "[render] WARNING: ${uid}.png is suspiciously small (${bytes} bytes)" >&2
      fail=$((fail + 1))
    else
      ok=$((ok + 1))
    fi
  else
    echo "[render] FAILED: ${uid}" >&2
    fail=$((fail + 1))
  fi
done <<< "${DASHBOARDS}"

echo "[render] done: ${ok} ok, ${fail} failed, output=${OUT}"
