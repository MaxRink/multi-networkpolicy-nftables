#!/bin/sh
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd -P)"

if [ -n "${KUBECTL:-}" ]; then
  build_manifest() {
    "${KUBECTL}" kustomize "$1"
  }
elif [ -n "${KUSTOMIZE:-}" ]; then
  build_manifest() {
    "${KUSTOMIZE}" build "$1"
  }
elif command -v kubectl >/dev/null 2>&1; then
  build_manifest() {
    kubectl kustomize "$1"
  }
elif command -v kustomize >/dev/null 2>&1; then
  build_manifest() {
    kustomize build "$1"
  }
else
  echo "kubectl or kustomize is required to generate manifests" >&2
  exit 1
fi

build_manifest "${ROOT}/config/manager/overlays/default" > "${ROOT}/deploy.yml"
build_manifest "${ROOT}/config/manager/overlays/e2e" > "${ROOT}/e2e/multi-network-policy-nftables-e2e.yml"
