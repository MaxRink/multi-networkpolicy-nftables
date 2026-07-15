# SR-IOV tc-flower dataplane backend

The controller ships two dataplane backends in one binary, selected **per pod
interface**:

- **nftables-in-netns** (default, unchanged): enters the pod network namespace
  and programs nftables. Correct for veth-style CNIs (macvlan, ipvlan, bond)
  whose traffic traverses the kernel netfilter path.
- **tc-flower-on-representor** (this document): programs `tc flower` filters on
  the host-side **VF representor** of an SR-IOV VF, offloaded to the NIC's
  embedded eSwitch. Required for SR-IOV VFs in **switchdev** mode, whose
  VF-to-VF traffic is switched inside the NIC and never reaches a kernel
  netfilter hook — so in-pod nftables cannot see it.

Both run side by side in the same reconcile pass. A single pod with both a
macvlan secondary and an SR-IOV VF secondary gets nftables on the veth and tc
flower on the representor; neither backend touches the other's interfaces.

## Backend selection (zero-config, per interface)

Selection is data-driven off the Multus `k8s.v1.cni.cncf.io/network-status`
annotation. If an interface carries a `DeviceInfo.Pci` block (VF PCI address /
representor), it is routed to the tc backend; otherwise to the nftables backend.

Operators must add the SR-IOV plugin name(s) to `--network-plugins` so SR-IOV
net-attach-defs pass the plugin-type filter (the default list is unchanged):

```
--network-plugins=macvlan,sriov,accelerated-bridge
```

## Prerequisites (out-of-band, not this daemon's job)

The tc backend requires the NIC to already be in switchdev mode with hardware
tc offload enabled. This is a node-provisioning concern; the daemon **detects**
its absence and errors loudly rather than silently leaving traffic unenforced.

- NIC: NVIDIA/Mellanox ConnectX-5 or newer (mlx5).
- eSwitch in switchdev mode: `devlink dev eswitch set pci/<pf> mode switchdev`.
- Hardware tc offload on the uplink: `ethtool -K <pf> hw-tc-offload on`.
- The DaemonSet mounts host `/sys` and `/proc` (already privileged, per node)
  and honours `--host-prefix` for all sysfs reads.

If the representor cannot be resolved or offload is off, the interface is
treated as **fail-closed** and reported via metrics/logs — never silently
allowed.

## Flags

| Flag | Default | Effect |
|------|---------|--------|
| `--enable-tc-backend` | `true` | Master switch for the whole SR-IOV tc backend. When `false`, every interface that would route to the tc backend is skipped — SR-IOV VFs are **not enforced** by this daemon and only the nftables backend runs. Use to opt out entirely on nodes/clusters where SR-IOV enforcement is not wanted. |
| `--tc-offload-mode` | `hardware` | `hardware` = `skip_sw` (hardware-only, fail-closed on insertion; production default). `software` = `skip_hw` (in-kernel software enforcement; for debugging/emulation without offload-capable HW). Ignored by the nftables backend. |
| `--tc-ct-mode` | `auto` | Stateful conntrack (CT) offload policy. `auto`: emit the CT pipeline where the NIC can hardware-offload it (SMFS steering), otherwise **degrade to the stateless pipeline** and log the lost stateful tracking (DMFS cannot offload CT). `require`: always emit CT; if the NIC cannot offload it the filters are rejected and the interface is left unenforced (fail-closed, stateful-or-nothing). `off`: never use CT (always stateless). Ignored by the nftables backend. |

### Graceful degradation — always enforce the maximum the hardware allows

The backend never silently does nothing. On each representor it enforces the
largest offloadable subset of the policy for the current NIC/steering config and
**logs, once, what was lost and how to recover it**:

- **Stateless allow/deny** (5-tuple, CIDR, ports, both IPv4 and IPv6) offloads on
  any switchdev NIC with `hw-tc-offload on`, regardless of steering mode.
- **Stateful CT** (established/related auto-accept) needs SMFS; under
  `--tc-ct-mode=auto` a DMFS/HMFS/unknown NIC degrades to stateless and logs an
  info-level "improvement available: switch to SMFS" line.
- If even the stateless filters cannot be offloaded (e.g. `hw-tc-offload off` in
  hardware mode) insertion fails **fail-closed** and the error is surfaced (log +
  `multinetworkpolicy_tc_filter_apply_errors_total{reason="skip_sw"}`), never
  silently allowed.

See [SR-IOV tc backend operations & debugging](sriov-tc-operations.md) for the
per-mode capability matrix, the exact log lines, metrics, and a debugging
runbook.

Disabling the backend cluster-wide:

```
--enable-tc-backend=false
```

## Stateful (CT) offload

To match the nftables backend's established/related accept, the tc backend can
emit mlx5 CT action chains (`ct` action + `ct_state` matching). Hardware CT
offload historically requires the mlx5 **SMFS** steering mode
(`devlink dev param set <pci> name flow_steering_mode value smfs`) and consumes
at least two eSwitch flow-table entries per tracked connection. The daemon runs
a preflight check and exposes CT-offload readiness and `ct_max_offloaded_conns`
as metrics.

## Deployment note: T-CaaS bm4x

The bm4x bare-metal profile **is** switchdev with VF representors, so the tc
backend applies directly. eSwitch mode is set out-of-band during imaging
(netplan `embedded-switch-mode: switchdev` + the `sriov-setup.sh` ansible step
running `devlink dev eswitch set ... mode switchdev` and
`ethtool -K <nic> hw-tc-offload on`); CNI is accelerated-bridge-cni on
ConnectX-5 (MCX512F) / ConnectX-6 Lx.

**Caveat — CT offload on bm4x as imaged:** bm4x hosts run the mlx5 default
**DMFS** steering mode, not SMFS. Hardware CT offload is associated with SMFS,
so on bm4x as currently imaged the CT chains are unlikely to hardware-offload.
Stateless policy (allow/deny by 5-tuple, CIDR, ports) offloads normally; only
the stateful established/related path is affected. See the steering-mode
section below for the recommendation to move bm4x to SMFS.

## Flow-steering modes (DMFS / SMFS / HMFS)

mlx5 selects how eSwitch steering rules are built via one runtime devlink
parameter, `flow_steering_mode` (values `dmfs` / `smfs` / `hmfs`):

- **DMFS** (Device-Managed Flow Steering) — the **default**. Steering entities
  are created and managed by **firmware**.
- **SMFS** (Software-Managed Flow Steering) — the **driver** builds the HW
  steering entities without firmware round-trips. The upstream kernel doc
  states plainly: *"SMFS mode is faster and provides better rule insertion rate
  compared to default DMFS mode."* It is the mode associated with OVS/ASAP²
  hardware offload and conntrack offload at scale.
- **HMFS** (Hardware-Managed Flow Steering) — newest, WQE-based, "millions of
  rules per second"; hardware floor is **ConnectX-6 Dx**.

Set (runtime):

```
devlink dev param set pci/<pf> name flow_steering_mode value smfs cmode runtime
```

Per-card support across our fleet (from the upstream mlx5 steering-format enum
and DR capability gate):

| Card | DMFS | SMFS | HMFS |
|------|------|------|------|
| ConnectX-4 / CX4-Lx | ✓ (only) | ✗ (no SW-steering format) | ✗ |
| ConnectX-5 | ✓ | ✓ | ✗ |
| ConnectX-6 Lx | ✓ | ✓ | not confirmed |
| ConnectX-6 Dx | ✓ | ✓ | ✓ |
| ConnectX-7 | ✓ | ✓ | ✓ |

### Should bm4x switch to SMFS?

**Recommendation: yes, move bm4x (CX5 + CX6 Lx + CX6 Dx) to SMFS** — all three
support it, it is faster for rule insertion, and it is the mode aligned with
CT/OVS hardware offload. Caveats to respect:

1. **Set SMFS before switchdev.** The mlx5 driver rejects `smfs` once the
   eSwitch is already in offloads mode ("Software managed steering is not
   supported when eswitch offloads enabled"). Provisioning must set
   `flow_steering_mode=smfs` **first**, then `devlink dev eswitch set … mode
   switchdev`. Wrong ordering is the main footgun.
2. **DMFS-only feature lost:** E-Switch *aggregated-affinity* matching is
   DMFS-only. Only relevant if bm4x uses multi-port/bond affinity matching in
   the FDB — verify it does not.
3. **CX5 feature/version floor:** CX5 uses the oldest SW-steering format and
   lacks some DR optimizations that CX6 Dx+ have; SMFS still works, but confirm
   the shipped MLNX_OFED/DOCA + kernel is recent enough for the CT ruleset.
4. **Host cost:** "software-managed" shifts steering-table construction to the
   host CPU/RAM. NVIDIA publishes no quantified overhead; do a canary rollout
   and watch driver CPU/RAM.

HMFS is **not** recommended fleet-wide: CX5 cannot do it, so SMFS is the common
denominator across bm4x.

> **Sourcing note:** the mode definitions, the "SMFS is faster" statement, and
> the per-card steering-format support are from the upstream kernel docs and
> mlx5 driver source (primary). "CT offload *requires* SMFS" is widely
> documented by NVIDIA (ASAP²) but the upstream tc CT path is not gated on
> steering mode — treat it as strong guidance, verify on the target
> OFED/firmware. `ct_max_offloaded_conns` and its `-ENOSPC` at
> `offloaded_flows >= ct_max_offloaded_conns * 2` behaviour are MLNX_OFED
> (out-of-tree) specifics; the BlueField default is 1,000,000 (adapter default
> not separately published — read `devlink dev param show`).

## Testing without hardware

The `test/emulation/` harness exercises discovery, control-plane netlink
round-trips, and software enforcement in CI without a Mellanox card. See
[`test/emulation/README.md`](../test/emulation/README.md) for the layered model
and what remains gated to real CX5+ silicon.
