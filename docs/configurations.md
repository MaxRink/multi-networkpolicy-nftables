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
