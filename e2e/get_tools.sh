#!/bin/sh
set -o errexit

# Pinned tool versions — update these explicitly
KIND_VERSION="v0.30.0"
KUBECTL_VERSION="v1.32.3"
JQ_VERSION="1.7.1"

# NOTE: The e2e YAML assets (e2e/multi-network-policy-nftables-e2e.yml, e2e/cni-install.yml)
# currently contain amd64-only manifests. Until those are updated for multi-arch,
# this script downloads amd64 binaries only on Linux regardless of host architecture.

# Pinned SHA256 checksums — update when bumping versions above
KIND_SHA256_AMD64="517ab7fc89ddeed5fa65abf71530d90648d9638ef0c4cde22c2c11f8097b8889"
KIND_SHA256_ARM64="7ea2de9d2d190022ed4a8a4e3ac0636c8a455e460b9a13ccf19f15d07f4f00eb"
KUBECTL_SHA256_AMD64="ab209d0c5134b61486a0486585604a616a5bb2fc07df46d304b3c95817b2d79f"
KUBECTL_SHA256_ARM64="6c2c91e760efbf3fa111a5f0b99ba8975fb1c58bb3974eca88b6134bcf3717e2"
JQ_SHA256_AMD64="5942c9b0934e510ee61eb3e30273f1b3fe2590df93933a93d7c58b81d19c8ff5"
JQ_SHA256_ARM64="4dd2d8a0661df0b22f1bb9a1f9830f06b6f3b8f7d91211a1ef5d7c4f06a8b4a5"

# Detect architecture
ARCH="$(uname -m)"
case "${ARCH}" in
    x86_64|amd64)  ARCH="amd64" ;;
    aarch64|arm64)  ARCH="arm64" ;;
    *)  echo "Unsupported architecture: ${ARCH}" >&2; exit 1 ;;
esac

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"

if [ ! -d bin ]; then
	mkdir bin
fi

# Helper: verify SHA256 checksum
verify_checksum() {
    local file="$1"
    local expected="$2"
    local actual
    actual="$(sha256sum "${file}" | awk '{print $1}')"
    if [ "${actual}" != "${expected}" ]; then
        echo "Checksum mismatch for ${file}:" >&2
        echo "  expected: ${expected}" >&2
        echo "  actual:   ${actual}" >&2
        rm -f "${file}"
        exit 1
    fi
    echo "Checksum OK: ${file}"
}

# Select per-arch checksums
case "${ARCH}" in
    amd64)
        KIND_SHA256="${KIND_SHA256_AMD64}"
        KUBECTL_SHA256="${KUBECTL_SHA256_AMD64}"
        JQ_SHA256="${JQ_SHA256_AMD64}"
        ;;
    arm64)
        KIND_SHA256="${KIND_SHA256_ARM64}"
        KUBECTL_SHA256="${KUBECTL_SHA256_ARM64}"
        JQ_SHA256="${JQ_SHA256_ARM64}"
        ;;
esac

echo "Downloading kind ${KIND_VERSION} (${OS}/${ARCH})..."
curl --fail --show-error -Lo ./bin/kind "https://github.com/kubernetes-sigs/kind/releases/download/${KIND_VERSION}/kind-${OS}-${ARCH}"
verify_checksum ./bin/kind "${KIND_SHA256}"
chmod +x ./bin/kind

echo "Downloading kubectl ${KUBECTL_VERSION} (${OS}/${ARCH})..."
curl --fail --show-error -Lo ./bin/kubectl "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/${OS}/${ARCH}/kubectl"
verify_checksum ./bin/kubectl "${KUBECTL_SHA256}"
chmod +x ./bin/kubectl

echo "Downloading jq ${JQ_VERSION} (${OS}/${ARCH})..."
case "${OS}" in
    linux)  JQ_PLATFORM="jq-linux-${ARCH}" ;;
    darwin) JQ_PLATFORM="jq-macos-${ARCH}" ;;
    *)      echo "Unsupported OS for jq: ${OS}" >&2; exit 1 ;;
esac
curl --fail --show-error -Lo ./bin/jq "https://github.com/jqlang/jq/releases/download/jq-${JQ_VERSION}/${JQ_PLATFORM}"
verify_checksum ./bin/jq "${JQ_SHA256}"
chmod +x ./bin/jq

echo "All tools downloaded successfully."
