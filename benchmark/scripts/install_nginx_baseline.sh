#!/usr/bin/env bash
# install_nginx_baseline.sh — Download and apply the pinned NGINX Ingress
# controller manifest for the kind-compatible external baseline.
#
# Usage:
#   bash benchmark/scripts/install_nginx_baseline.sh
#
# Environment variables:
#   NGINX_VERSION   Ingress-NGINX controller tag (default: controller-v1.11.2)
#   SKIP_SHA256     Set to "1" to skip checksum verification
#
# After applying the upstream controller, run:
#   kubectl apply -f benchmark/manifests/baseline-nginx-controller.yaml
# to patch the NodePorts to 30180/30143.

set -euo pipefail

NGINX_VERSION="${NGINX_VERSION:-controller-v1.11.2}"
PINNED_SHA256="dc850e38ca4abcb08625f1601f0656c81b6b5e34cc23d5458c332060732c14e0"
UPSTREAM_URL="https://raw.githubusercontent.com/kubernetes/ingress-nginx/${NGINX_VERSION}/deploy/static/provider/kind/deploy.yaml"

echo "==> Installing NGINX Ingress controller (${NGINX_VERSION}) for kind"
echo "    URL: ${UPSTREAM_URL}"

TMPFILE="$(mktemp /tmp/nginx-ingress-kind-XXXXXX.yaml)"
trap 'rm -f "${TMPFILE}"' EXIT

echo "==> Downloading manifest..."
curl -fsSL "${UPSTREAM_URL}" -o "${TMPFILE}"

if [[ "${SKIP_SHA256:-0}" != "1" ]]; then
  echo "==> Verifying SHA256..."
  ACTUAL_SHA256="$(shasum -a 256 "${TMPFILE}" | awk '{print $1}')"
  if [[ "${ACTUAL_SHA256}" != "${PINNED_SHA256}" ]]; then
    echo "ERROR: SHA256 mismatch for ${NGINX_VERSION}"
    echo "  expected: ${PINNED_SHA256}"
    echo "  actual:   ${ACTUAL_SHA256}"
    echo "If you intentionally changed NGINX_VERSION, update PINNED_SHA256 in this script."
    exit 1
  fi
  echo "    SHA256 OK: ${ACTUAL_SHA256}"
else
  echo "    SHA256 verification skipped (SKIP_SHA256=1)"
fi

echo "==> Applying upstream manifest..."
kubectl apply -f "${TMPFILE}"

echo "==> Waiting for ingress-nginx controller to become ready (timeout 120s)..."
kubectl rollout status deployment/ingress-nginx-controller \
  -n ingress-nginx --timeout=120s

echo ""
echo "==> Now apply the NodePort patch (30180/http, 30143/https):"
echo "    kubectl apply -f benchmark/manifests/baseline-nginx-controller.yaml"
echo ""
echo "Done. NGINX Ingress controller (${NGINX_VERSION}) is running."
