#!/usr/bin/env bash
# archive_results.sh — Package a single run directory into a compressed tarball.
#
# Usage:
#   archive_results.sh <run-directory>
#   archive_results.sh -h
#
# The run-directory must be a direct child of benchmark/results/ (not the
# results root itself).
#
# Output: benchmark/results/archive/<basename>.tar.gz
# Prints the archive path on success.

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

RUN_DIR="${1%/}"  # strip trailing slash

if [[ ! -d "${RUN_DIR}" ]]; then
  echo "ERROR: directory does not exist: ${RUN_DIR}" >&2
  exit 1
fi

PARENT="$(dirname "${RUN_DIR}")"
BASENAME="$(basename "${RUN_DIR}")"

# Guard: must not be the results root itself
if [[ "${BASENAME}" == "results" ]]; then
  echo "ERROR: argument must be a run directory under benchmark/results/, not results itself" >&2
  exit 1
fi

ARCHIVE_DIR="${PARENT}/archive"
mkdir -p "${ARCHIVE_DIR}"

ARCHIVE="${ARCHIVE_DIR}/${BASENAME}.tar.gz"

echo "[archive] creating ${ARCHIVE}..."
tar -C "${PARENT}" -czf "${ARCHIVE}" "${BASENAME}"

echo "[archive] done: ${ARCHIVE}"
