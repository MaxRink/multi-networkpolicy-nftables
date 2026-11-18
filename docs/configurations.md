## Multi-networkpolicy-nftables Configurations


### Command Line Options

Most command line options have description in help, so please execute with `--help` to see the option.

```
$ ./multi-networkpolicy-nftables --help
```

### Advanced Options

#### Node identity

The configured hostname must match the Kubernetes Node name exactly, including
any domain suffix. Set `--hostname-override` to that name when the operating
system hostname differs.
Nodes with the same short hostname in different domains are distinct.
Only pods scheduled to the exact local node are resolved through the local CRI
socket; remote pod interfaces remain available for peer selection.

#### Host paths used by the DaemonSet

The sample DaemonSet mounts the host filesystem at `/host`. Keep the
controller flag aligned with that mount:

```
--host-prefix=/host
```

If a cluster uses a CRI socket other than the default shown in `deploy.yml`,
set `--container-runtime-endpoint` to the absolute host socket path. Do not use
a `unix://` URL here; the controller adds the CRI transport internally.

`deploy.yml` is generated from `config/manager/overlays/default`. The e2e
install manifest is generated from the same base plus
`config/manager/overlays/e2e`, so host paths, RBAC, and shared mounts remain
aligned between normal deployments and tests.

#### Logging

The controller logs through klog to stderr. Two flags set the level and the
format:

| Flag | Values | Default |
|---|---|---|
| `--log-level` | `debug`, `info`, `warn`, `error` | empty: `-v` applies (default `0`) |
| `--log-format` | `text`, `json` | `text` |

Any other value is rejected at start-up. `--log-level` sets the klog verbosity
and overrides `-v`:

| `--log-level` | klog | Adds |
|---|---|---|
| `error` | `-v=0` | errors, warnings, start-up and other unconditional lines |
| `warn` | `-v=0` | the same as `error` |
| `info` | `-v=2` | `-v=1` and `-v=2` lines: pods found on the node, removal of legacy rules |
| `debug` | `-v=4` | `-v=3` and `-v=4` lines: rule generation per pod, pods without relevant interfaces |

klog has no level that hides its unconditional info lines, so `warn` and
`error` behave the same. Lines at `-v=5` and higher (network namespaces, interfaces, generated
nftables rules) need `-v` and no `--log-level`. Without either flag the controller logs as
before, at `-v=0`.

`--log-format=text` is the klog text format. `--log-format=json` writes one
JSON object per line through the standard library `log/slog` handler:

```
{"time":"2026-11-17T19:34:21.95Z","level":"INFO","msg":"Starting manager for node node-1"}
```

In JSON, `level` is `ERROR` or `INFO`; klog warnings are written as `INFO`.
A line at `-v=n` has the slog level `-n`, which slog writes as `DEBUG+2` for
`n=2`, `DEBUG` for `n=4` and `DEBUG-4` for `n=8`. Filtering by verbosity
happens in klog before the handler, as with text.

In JSON mode an invalid option or a start-up failure is logged once as
JSON (`Execute failed: ...`), without cobra's text usage. Only a flag that
cannot be parsed at all, for example an unknown flag, is still reported by
cobra as text with the usage, because it happens before the format is set.

The lines of the shutdown cleanup (`cleanup:` and `flush-cleanup`) go through klog
like all others, so they follow the format and the verbosity.

#### Compatibility notes

Pod iptables state is no longer persisted. The old `--pod-iptables` flag is
still accepted as a hidden, deprecated no-op so older manifests do not fail at
startup while they are being migrated.

File-mounted custom rule ConfigMaps are not used by the nftables controller.
Use the command line options below for supported exceptional traffic handling.

#### Add exceptional IP prefix address to accept

Some networks may require accepting traffic from/to specific address prefixes for the network, such as multicast address (all routers multicast address, link-local address and so on). You can configure `--allow-src-prefix` and `--allow-dst-prefix` to specify which prefix should be accepted (even though network policy does not have it). Both options accept a comma-separated CIDR list.

```
--allow-src-prefix=fe80::/10
--allow-dst-prefix=fe80::/10,ff00::/8
```

#### Default deny

By default, a pod interface that no MultiNetworkPolicy selects allows all
traffic, as with Kubernetes NetworkPolicy. `--default-deny` denies traffic on
such interfaces unless a policy allows it:

| `--default-deny` | Pods that no policy selects |
|---|---|
| `none` (default) | all traffic allowed |
| `ingress` | incoming traffic denied, outgoing allowed |
| `egress` | outgoing traffic denied, incoming allowed |
| `all` | both directions denied |

Any other value is rejected at start-up. The flag is set for the whole
DaemonSet; the sample manifests set `--default-deny=none`.

Scope: the controller only programs pods on its node that have at least one
interface on a managed network, meaning a net-attach-def whose CNI plugin type
is listed in `--network-plugins`. The deny covers those interfaces only.
Loopback, the cluster network interface (`eth0`), and interfaces of other
networks are never filtered. Host-network pods are never touched.

Policies stay additive. For each direction, default deny gives a pod the same
rules as a policy that selects the pod and allows nothing in that direction:
a final drop in the pod's `multi-ingress` or `multi-egress` chain. A policy
that selects the pod adds its rules ahead of that drop and opens exactly what
it allows. Once a policy of a direction selects a pod, the flag makes no
difference for that pod and direction.

In a denied direction, the following stays allowed. These are the rules of the
`multi-ingress-common` and `multi-egress-common` chains, which also apply to
pods that policies select:

- replies and related traffic of allowed connections (`ct state established,related`),
  in both directions;
- IPv6 neighbour discovery: ICMPv6 router solicitation, router advertisement,
  neighbour solicitation and neighbour advertisement. With `--accept-icmpv6`,
  all ICMPv6;
- all ICMP with `--accept-icmp`;
- sources in `--allow-src-prefix` (ingress) and destinations in
  `--allow-dst-prefix` (egress);
- ARP, which is not IP traffic and does not pass the nftables `inet` hooks.

DHCP is not special-cased. Clients that use packet sockets, such as the CNI
`dhcp` IPAM daemon, are not affected. A DHCP client that uses UDP sockets in
the pod needs its DHCP traffic allowed by policies, or by `--allow-src-prefix`
and `--allow-dst-prefix`.

A policy whose rules cannot be built, for example one with a port out of
range or an invalid CIDR, still selects its pods but allows nothing; the
daemon logs the error on every reconcile. A named port is valid: one that
matches no container port allows nothing on that port only.

Default deny filters the pod's own IP traffic in the `input` and `output`
hooks of the pod's network namespace. It does not filter traffic that the pod
forwards between its interfaces. It is not a boundary against the pod itself:
a container with `CAP_NET_RAW` can send and receive frames on a packet socket,
and a container with `CAP_NET_ADMIN` can change the pod's nftables rules or
rename its interfaces. Drop these capabilities from pods that default deny
must contain.

The drop rule is written in the same nftables transaction as the pod's tables,
chains and interface jumps, so a pod is denied from the first reconcile on.
Adding or removing a policy rewrites the chain in place and keeps the drop
last. As with policies, enforcement starts with the first reconcile after the
pod is running, and the controller removes all of its rules from pods when it
shuts down or is uninstalled, so pods are open while the DaemonSet is not
running.

Rollout:

1. With `--default-deny=none`, write the allow policies for each managed
   network. A pod that a policy selects is already denied everything else, so
   the policies can be tested before the flag changes anything.
2. For networks that are not ready yet, add a policy that selects all pods and
   allows everything on that network, for example:

   ```yaml
   apiVersion: k8s.cni.cncf.io/v1beta1
   kind: MultiNetworkPolicy
   metadata:
     name: allow-all
     namespace: my-namespace
     annotations:
       k8s.v1.cni.cncf.io/policy-for: my-namespace/my-network
   spec:
     podSelector: {}
     policyTypes: [Ingress, Egress]
     ingress:
     - {}
     egress:
     - {}
   ```

3. Enable `--default-deny=ingress`, check the drop counters
   (`nft list chain inet multi-networkpolicy-filter multi-ingress` in the pod's
   network namespace), then enable `all`.
4. Remove the allow-all policies one network at a time.
