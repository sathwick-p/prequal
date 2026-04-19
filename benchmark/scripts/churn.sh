#!/usr/bin/env bash

set -euo pipefail

if [[ $# -ne 6 ]]; then
  echo "usage: $0 <namespace> <deployment> <low_replicas> <high_replicas> <pause_seconds> <iterations>" >&2
  exit 1
fi

namespace="$1"
deployment="$2"
low_replicas="$3"
high_replicas="$4"
pause_seconds="$5"
iterations="$6"

for ((i = 0; i < iterations; i++)); do
  kubectl -n "$namespace" scale deployment "$deployment" --replicas="$high_replicas"
  sleep "$pause_seconds"
  kubectl -n "$namespace" scale deployment "$deployment" --replicas="$low_replicas"
  sleep "$pause_seconds"
done
