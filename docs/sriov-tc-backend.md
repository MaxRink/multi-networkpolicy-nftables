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
**DMFS** steering mode, not SMFS. Hardware CT offload requires SMFS, so on bm4x
as currently imaged the CT chains will **not** be hardware-offloaded. Stateless
policy (allow/deny by 5-tuple, CIDR, ports) offloads normally; only the
stateful established/related path is affected. To hardware-offload CT on bm4x,
switch the uplink to SMFS during provisioning — otherwise leave CT policies off
these nodes or accept that they are not HW-enforced.

## Testing without hardware

The `test/emulation/` harness exercises discovery, control-plane netlink
round-trips, and software enforcement in CI without a Mellanox card. See
[`test/emulation/README.md`](../test/emulation/README.md) for the layered model
and what remains gated to real CX5+ silicon.
