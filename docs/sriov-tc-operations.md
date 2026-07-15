# SR-IOV tc-flower backend — operations & debugging

Operational guide for the SR-IOV tc-flower dataplane backend: what each
operating mode enforces and its limits, the log lines the daemon emits, the
metrics it exports, and a step-by-step debugging runbook.

> **Confidence markers.** Statements are marked **[primary]** (upstream kernel
> docs / mlx5 source / man pages), **[secondary]** (NVIDIA docs, forums, OFED
> source mirrors), or **[unconfirmed]** (engineering inference or widely-repeated
> but not verified against a primary source). Verify **[unconfirmed]** items on
> your exact driver/firmware before relying on them. See
> [sriov-tc-backend.md](sriov-tc-backend.md) for the sourcing note and the
> DMFS/SMFS/HMFS comparison.

## 1. Operating modes and exactly what each enforces

Two orthogonal knobs decide the dataplane behaviour: `--tc-offload-mode`
(where the filter runs) and `--tc-ct-mode` (stateful vs stateless). The backend
always enforces the **maximum subset the hardware+config supports** and logs the
rest.

### 1a. `--tc-offload-mode`

| Mode | tc flag | Where it runs | When to use | Limit / caveat |
|------|---------|---------------|-------------|----------------|
| `hardware` (default) | `skip_sw` | NIC eSwitch only | Production on ConnectX switchdev NICs | **Fail-closed**: if the NIC cannot offload a filter, the kernel *rejects* the insertion — the rule is NOT silently enforced in software [primary: tc-flower(8)]. Requires `hw-tc-offload on`. |
| `software` | `skip_hw` | host kernel datapath | CI/emulation (veth, netdevsim), non-offload NICs, debugging | No hardware acceleration; every packet is processed in software on the host CPU. VF-to-VF traffic switched *inside* the NIC eSwitch is **not seen** by the host kernel, so on a real switchdev NIC software mode does **not** enforce inter-VF traffic — it is for emulation/veth only. **[unconfirmed]** exact visibility depends on driver. |
| `auto` | *(neither)* | kernel decides | — | **Not supported** (rejected at startup): managed-filter detection needs an explicit `skip_sw`/`skip_hw` flag. |

### 1b. `--tc-ct-mode` (stateful conntrack)

CT offload adds established/related auto-accept (like the nft backend's
`allowConntracked`), so a policy only needs to permit the NEW direction. It
requires **SMFS** steering [secondary: NVIDIA ASAP²; the upstream tc CT path is
not gated on steering mode — **[unconfirmed]** as a hard requirement].

| Mode | Behaviour on SMFS | Behaviour on DMFS / HMFS / unknown | Use when |
|------|-------------------|------------------------------------|----------|
| `auto` (default) | Stateful CT pipeline | **Degrade to stateless** + log improvement. Stateless allow/deny still fully enforced. | You want the best available everywhere without per-node tuning. |
| `require` | Stateful CT pipeline | **Fail-closed**: CT filters rejected, interface unenforced. | SMFS fleet that must be stateful-or-error. |
| `off` | Stateless | Stateless | You never want CT (e.g. pure allow/deny policies). |

**What "stateless" loses vs "stateful":** with a stateless pipeline you must
write explicit rules for **both** directions of a conversation (e.g. an egress
allow to a service AND an ingress allow for its replies), because return packets
are matched as fresh 5-tuples, not as "established". Stateful CT lets the reply
be auto-accepted. Stateless is otherwise fully functional for allow/deny by
5-tuple, CIDR, and port ranges, IPv4 and IPv6.

### 1c. Per-card capability summary (bm4x fleet + neighbours)

| Card | Stateless offload | Stateful CT offload (SMFS) | HMFS |
|------|-------------------|----------------------------|------|
| ConnectX-4 / Lx | ✓ (DMFS only) | ✗ (no SMFS) | ✗ |
| ConnectX-5 | ✓ | ✓ (set SMFS) | ✗ |
| ConnectX-6 Lx | ✓ | ✓ (set SMFS) | **[unconfirmed]** |
| ConnectX-6 Dx | ✓ | ✓ (set SMFS) | ✓ |
| ConnectX-7 | ✓ | ✓ (set SMFS) | ✓ |

See [sriov-tc-backend.md § steering modes](sriov-tc-backend.md#flow-steering-modes-dmfs--smfs--hmfs)
for sources.

## 2. Startup log summary

When the tc backend is enabled the daemon logs a one-shot host summary at
startup (`tcflower startup: …`). It enumerates each Mellanox/NVIDIA SR-IOV PF and
reports every offload-relevant limit. Expected lines and their meaning:

```
tcflower startup: SR-IOV tc-flower backend ENABLED (offload-mode=hardware, ct-mode=auto); found 2 Mellanox/NVIDIA SR-IOV PF(s) under /host/sys
tcflower startup: PF 0000:03:00.0 [ConnectX-6 Lx, driver=mlx5_core] switchdev=true vfs=8/16
tcflower startup: PF 0000:03:00.0 flow_steering_mode=dmfs
tcflower startup: PF 0000:03:00.0 steering mode is "dmfs" (not smfs); conntrack offload is NOT available, so --tc-ct-mode=auto DEGRADES this PF to STATELESS enforcement ... IMPROVEMENT: switch to SMFS ...
```

Warning lines you must act on:

| Log fragment | Meaning | Fix |
|--------------|---------|-----|
| `no Mellanox/NVIDIA SR-IOV physical function found` | No mlx5 SR-IOV NIC visible in sysfs | Expected on non-SR-IOV nodes → set `--enable-tc-backend=false`. Otherwise check `/sys` is mounted and `--host-prefix`. |
| `is NOT in switchdev mode` | PF eSwitch is in legacy mode; VFs have no representors | `devlink dev eswitch set pci/<pf> mode switchdev` (during provisioning). |
| `hw-tc-offload is OFF` | tc offload disabled on the uplink | `ethtool -K <uplink> hw-tc-offload on`. In hardware mode this is fatal (filters rejected). |
| `--tc-ct-mode=require but PF … steering mode is "dmfs"` | CT required but cannot offload → interface unenforced | Switch to SMFS **before** switchdev, or use `--tc-ct-mode=auto`. |
| `could not read flow_steering_mode` | devlink unavailable/unprivileged | Ensure the pod is privileged and `devlink` is in the image; else CT auto-degrades. |

## 3. Metrics

All exported on the controller-runtime `/metrics` endpoint (Prometheus). Prefix
`multinetworkpolicy_tc_`.

| Metric | Type | Labels | Meaning / alert |
|--------|------|--------|-----------------|
| `filters_installed` | gauge | representor, direction | Desired/installed managed filters. Drops to 0 when policies removed. |
| `filters_in_hw` | gauge | representor, direction | Filters confirmed offloaded to the eSwitch (`in_hw`). |
| `filters_not_in_hw` | gauge | representor, direction | Filters present but NOT offloaded. **Alert if > 0**: policy is not enforced in hardware. |
| `filter_apply_errors_total` | counter | representor, reason (`add`/`delete`/`skip_sw`) | **`reason="skip_sw"` is the fail-closed offload-rejection signal — alert on any increase.** |
| `representor_resolution_errors_total` | counter | reason | A pod VF could not be mapped to a representor (interface left unenforced). |
| `offload_ready` | gauge | representor | 1 if the representor is present and offload-ready. |
| `ct_offload_ready` | gauge | representor | 1 if the eSwitch is SMFS (CT-offload capable), 0 if not. **0 ⇒ running degraded/stateless under auto.** |
| `ct_max_offloaded_conns` | gauge | representor | Hardware CT table capacity (mlx5 `ct_max_offloaded_conns`). The effective flow ceiling is `2 ×` this (two entries/connection) [secondary: MLNX_OFED `tc_ct.c`]. |
| `reconcile_duration_seconds` | histogram | representor | Time to enforce one representor. |

Recommended alerts: `filters_not_in_hw > 0`, `increase(filter_apply_errors_total{reason="skip_sw"}[5m]) > 0`,
`ct_offload_ready == 0` (if you expect stateful), and CT utilisation approaching
`ct_max_offloaded_conns × 2`.

## 4. Debugging runbook

Work top-down; each step narrows the failure to a layer.

### Step 0 — is the interface even routed to the tc backend?

- The pod's Multus `network-status` annotation must carry a `device-info`/`pci`
  block for the VF. Without it the interface is handled by the **nftables**
  backend, not tc.
- Operators must include the SR-IOV plugin name in `--network-plugins`
  (e.g. `sriov`, `accelerated-bridge`). Check the daemon flags.

```sh
kubectl get pod <pod> -o jsonpath='{.metadata.annotations.k8s\.v1\.cni\.cncf\.io/network-status}' | jq
```

### Step 1 — is the NIC in switchdev with offload on?

```sh
# eSwitch mode (want: switchdev)
devlink dev eswitch show pci/0000:03:00.0
# hw offload (want: hw-tc-offload: on)
ethtool -k <uplink> | grep hw-tc-offload
# steering mode (want smfs for CT; dmfs is fine for stateless)
devlink dev param show pci/0000:03:00.0 name flow_steering_mode
```

### Step 2 — did the representor resolve?

```sh
# the daemon logs "resolve representor for interface ...": on failure it also
# bumps representor_resolution_errors_total{reason=...}. Confirm the rep exists:
ls -l /sys/class/net/*/phys_port_name    # VF reps read e.g. "pf0vf3"
cat /sys/class/net/<rep>/phys_switch_id  # must match the PF uplink's
```

### Step 3 — are the filters installed and offloaded?

```sh
# list filters on the representor (both parents):
tc -s filter show dev <rep> ingress
tc -s filter show dev <rep> egress
# key things to look for per filter:
#   in_hw            -> offloaded to the eSwitch (GOOD)
#   not_in_hw        -> present but NOT offloaded (policy not HW-enforced)
#   skip_sw          -> hardware-only (our hardware mode)
# JSON form for scripting:
tc -j -s filter show dev <rep> ingress | jq '.[] | {pref, in_hw, not_in_hw}'
```

- Filters present with `in_hw` → enforcement is live in hardware. 
- Filters present but `not_in_hw` → the NIC accepted the rule for bookkeeping but
  did not program it (steering table full, unsupported match, firmware). Check
  `dmesg` for mlx5 errors and `filters_not_in_hw`.
- No filters at all → resolution or apply failed; see the daemon log and
  `filter_apply_errors_total`.

### Step 4 — insertion rejected (fail-closed)

If `filter_apply_errors_total{reason="skip_sw"}` is climbing, the kernel rejected
a hardware-only filter (`EOPNOTSUPP`/`ENOTSUPP`). Common causes:

- `hw-tc-offload off` → `ethtool -K <uplink> hw-tc-offload on`.
- Steering table exhausted → check scale vs `ct_max_offloaded_conns × 2` and the
  (unpublished) flower table size; `dmesg | grep mlx5`.
- Match key unsupported by the firmware/steering-format (older CX5) → try
  `--tc-offload-mode=software` to confirm the *translation* is correct, then
  raise with NVIDIA.

### Step 5 — CT not offloading

- `ct_offload_ready == 0` and steering is `dmfs` → expected under `auto`
  (degraded to stateless). To get stateful CT: set SMFS **before** switchdev
  (`devlink dev param set pci/<pf> name flow_steering_mode value smfs cmode
  runtime`) at provisioning time. **The driver rejects SMFS once the eSwitch is
  already in offloads mode** [secondary: MLNX_OFED devlink.c], so ordering
  matters — this usually means re-imaging or a careful un-switchdev → SMFS →
  switchdev dance.
- New connections dropped at scale → CT table full (`-ENOSPC` at
  `num_offloaded_flows >= ct_max_offloaded_conns * 2`) [secondary]. Raise
  `ct_max_offloaded_conns` or reduce connection churn.

### Step 6 — reproduce translation without hardware

To prove the policy→flower translation independent of the NIC:

```sh
# software enforcement on a veth pair (no offload):
sudo bash test/emulation/veth-flower-enforcement.sh
# netdevsim switchdev topology (representor discovery + control plane):
sudo bash test/emulation/netdevsim-discovery.sh
```

See [test/emulation/README.md](../test/emulation/README.md) for the full layered
harness and what remains hardware-gated.

## 5. Known limits & caveats (consolidated)

- **Direction inversion** (representor ingress ↔ policy egress and vice-versa) is
  the highest-risk correctness area and is validated only on real switchdev
  hardware [primary: kernel representors.rst]. If allow/deny appears mirrored,
  suspect this first.
- **DPDK/VFIO VFs** not bound to a kernel driver in the pod are still enforced at
  the *host representor* (that is the whole point of this backend), but the pod
  side is invisible to the host — expected.
- **software mode on real switchdev NICs** does not see eSwitch-switched
  VF-to-VF traffic **[unconfirmed]** — software mode is for emulation/veth and
  non-offload NICs, not a fallback for production inter-VF policy.
- **HMFS + CT**: HMFS CT-offload support is **[unconfirmed]**; `auto` treats HMFS
  as non-CT-offloadable and degrades to stateless rather than emit filters that
  might be rejected. Use `--tc-ct-mode=require` to force an attempt if you have
  verified HMFS CT on your firmware.
- **ct_max_offloaded_conns** default is **[unconfirmed]** for adapters (the
  BlueField provisioning default is 1,000,000 [secondary]); read the live value
  with `devlink dev param show`.
- **Max flower rules / priorities per card**: NVIDIA publishes no hard number
  [primary: absence confirmed]; treat as measured, watch `filters_installed` and
  `filters_not_in_hw`.
- **`ct_max_offloaded_conns` and the `×2` ENOSPC behaviour are MLNX_OFED
  (out-of-tree)** [secondary]; a stock upstream kernel may differ.
