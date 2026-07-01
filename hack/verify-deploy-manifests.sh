#!/bin/sh
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd -P)"
TMPDIR="$(mktemp -d)"
trap 'rm -rf "${TMPDIR}"' EXIT

cp "${ROOT}/deploy.yml" "${TMPDIR}/deploy.yml"
cp "${ROOT}/e2e/multi-network-policy-nftables-e2e.yml" "${TMPDIR}/multi-network-policy-nftables-e2e.yml"

"${ROOT}/hack/update-deploy-manifests.sh"

status=0
if ! diff -u "${TMPDIR}/deploy.yml" "${ROOT}/deploy.yml"; then
  status=1
fi
if ! diff -u "${TMPDIR}/multi-network-policy-nftables-e2e.yml" "${ROOT}/e2e/multi-network-policy-nftables-e2e.yml"; then
  status=1
fi

if [ "${status}" -ne 0 ]; then
  echo "generated manifests are stale; run hack/update-deploy-manifests.sh" >&2
fi

exit "${status}"
