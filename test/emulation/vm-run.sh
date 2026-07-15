#!/usr/bin/env bash
#
# Copyright 2026 Deutsche Telekom AG.
# Licensed under the Apache License, Version 2.0.
#
# vm-run.sh — run the tc-flower e2e / emulation suite inside a real VM kernel
# via KVM, so netdevsim (a kernel module), switchdev representors, and the
# software-datapath flower enforcement run against a full, real kernel instead
# of a shared container kernel.
#
# This is the "closest to reality without a ConnectX card" tier: a VM lets us
#   (a) modprobe netdevsim and exercise switchdev eswitch + VF representors, and
#   (b) later boot a *patched* kernel (netdevsim tc.c that actually enforces
#       offloaded flower rules — "Layer D") to close the offloaded-drop gap.
#
# It uses virtme-ng (https://github.com/arighi/virtme-ng), the standard tool for
# booting a kernel in a throwaway VM that reuses the host root filesystem, so the
# Go toolchain and repo checkout are available inside with no image building.
#
# Requirements: /dev/kvm (nested virtualization), virtme-ng (`vng`) + qemu, and
# a kernel to boot (the host kernel by default; set VNG_KERNEL to a built kernel
# tree / bzImage for Layer D). Exits 0 (skip) when KVM or vng are unavailable,
# so it is safe to run where nested virtualization is not offered.
#
# Usage:  bash test/emulation/vm-run.sh
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${HERE}/../.." && pwd)"

skip() { echo "SKIP: $*"; exit 0; }

if [[ ! -e /dev/kvm ]]; then
  skip "no /dev/kvm (KVM/nested virtualization unavailable); cannot boot a VM"
fi
if ! command -v vng >/dev/null 2>&1; then
  skip "virtme-ng (vng) not installed; install with 'pip install virtme-ng' on a KVM runner"
fi

# The in-VM steps live in a sibling script so there is no fragile shell quoting
# across the host->guest boundary; vng runs it with the repo path in the env.
GUEST_SCRIPT="${HERE}/vm-guest.sh"
if [[ ! -x "${GUEST_SCRIPT}" ]]; then
  skip "guest script ${GUEST_SCRIPT} missing/not executable"
fi

KERNEL_ARG=()
if [[ -n "${VNG_KERNEL:-}" ]]; then
  KERNEL_ARG=(--kimg "${VNG_KERNEL}")
  echo "== booting custom kernel: ${VNG_KERNEL} =="
else
  echo "== booting host kernel in a VM =="
fi

# --user root: run privileged in the guest (modprobe, tc, netns all need it).
# The host rootfs (incl. the repo checkout and the Go toolchain) is shared, so
# REPO_ROOT resolves the same inside the VM.
exec vng --run "${KERNEL_ARG[@]}" --user root --cpus 2 --memory 2G \
  --env "REPO_ROOT=${REPO_ROOT}" -- \
  bash "${GUEST_SCRIPT}"
