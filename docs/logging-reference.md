# Logging reference

This page lists what `multi-networkpolicy-nftables` logs: the binary, the flags that set the level and the format, every log message with its arguments, and which messages can hold personal data. `TestLoggingReference` in `docs/logging_test.go` fails when the message catalog and the log calls in `cmd/` and `pkg/` differ.

## Binaries

| Binary | Built from | Runs as | Writes to |
|---|---|---|---|
| `multi-networkpolicy-nftables` | `cmd/multi-networkpolicy-nftables` | container of the DaemonSet `multi-networkpolicy-ds-amd64` in `kube-system` (`deploy.yml`) | stderr |

The repository builds no other binary.

## Levels and format

`--log-level` (`debug`, `info`, `warn`, `error`) maps onto klog verbosity: `debug` is `-v=4`, `info` is `-v=2`, `warn` and `error` are `-v=0`. Without it, `-v` applies (default `0`). `--log-format` is `text` (klog, default) or `json` (standard library `log/slog`). [configurations.md](configurations.md#logging) describes both flags.

| Catalog level | Shown at |
|---|---|
| `error`, `warning`, `info` | always |
| `V(1)`, `V(2)` | `--log-level=info` or `debug`, or `-v=1`/`-v=2` |
| `V(4)` | `--log-level=debug`, or `-v=4` |
| `V(6)`, `V(8)`, `V(10)` | `-v=6`, `-v=8` or `-v=10` only |

`deploy.yml` sets no level, so a deployment logs the `error`, `warning` and `info` lines.

## Line format

`text`, the klog header and the formatted message:

```
I1117 19:39:11.950704    7565 main.go:204] Starting manager for node node-1
```

`json`, one object per line with `time`, `level` and `msg`; the message is formatted with its arguments:

```
{"time":"2026-11-17T19:39:11.96Z","level":"INFO","msg":"Starting manager for node node-1"}
```

The calls are printf-style, so the arguments are part of `msg`, not separate fields. A klog warning is written as `INFO` in JSON.

With `--log-format=json`, one kind of output is still plain text: **flags that cannot be parsed** (an unknown flag, a value of the wrong type). Cobra reports them as text with the usage, followed by a klog text line `Execute failed: ...`, because parsing happens before the format is set. An invalid value of a parsed option (`--log-level=verbose`, a relative `--container-runtime-endpoint`) is logged once as JSON. The shutdown cleanup lines (`cleanup:` and `flush-cleanup`, from `debugLog` in `pkg/controller/cleanup_linux.go`) go through klog only and are JSON like all others.

## Lines of libraries

controller-runtime logs through `klog.NewKlogr()` and so follows `-v` and the format. Its reconcile lines are structured and carry `controller`, `controllerGroup`, `controllerKind`, `Node`, `namespace`, `name` and `reconcileID`, and `Reconciler error` lines carry `error`. The reconciled object is the local Node. client-go logs through klog at the same verbosity. These lines are not in the catalog.

Personal data in these lines: `Node`, `namespace` and `name` hold the node name (`none`). The `error` of a `Reconciler error` line, written at the default level, repeats the errors of the reconciler: pod namespaces and names (`tenant-pii`, `tenant-user`), and from failed pod-info lookups and rule updates, network status, addresses and rule contents (`customer-data`, `end-customer`).

## Personal data

The classes and data subjects are those of the T-CaaS logging contract.

| Personal data | In this binary |
|---|---|
| `none` | node names, table names, counts, settings of the operator |
| `tenant-pii` | names chosen by tenant users: pod and namespace names, MultiNetworkPolicy and NetworkAttachmentDefinition names, label selectors, and the chain, set and rule names derived from them |
| `customer-data` | pod IP and MAC addresses, network status, and nftables rule contents, which hold `ipBlock` CIDRs |

| Data subject | In this binary |
|---|---|
| `tenant-user` | staff of a tenant, who name pods, namespaces and policies |
| `end-customer` | people behind the traffic of tenant pods; pod addresses and `ipBlock` CIDRs identify them or their peers |

A message that can hold several kinds lists the one with the highest need for protection.

- **Default level** (`-v=0`): pod and namespace names in errors, in `Execute failed` and in the shutdown cleanup lines (`tenant-pii`). Error text of failed rule updates and network status lookups, also in `Reconciler error`, can repeat addresses (`customer-data`, `end-customer`).
- **`--log-level=info`** (`-v=2`) adds pod names on the node and legacy rule names (`tenant-pii`).
- **`--log-level=debug`** (`-v=4`) adds the pods rules are generated for (`tenant-pii`).
- **`-v=6` and higher** add pod interfaces and network status with IP and MAC addresses (`customer-data`, `end-customer`), and policy, chain and selector names.
- **Never logged**: Secrets, kubeconfig contents, packet payloads.

## Message catalog

Every log call of `cmd/` and `pkg/`, by level, with the meaning of its format arguments and its personal-data class and data subject. A message used by several calls has one row.

<!-- BEGIN MESSAGE CATALOG -->
| Level | Message | Arguments | Personal data | Data subject |
|---|---|---|---|---|
| error | `bad label selector for policy [%s]: %v` | policy namespace/name, parse error | tenant-pii | tenant-user |
| error | `cannot close pod (%s/%s) netns: %v` | pod namespace, pod name, error | tenant-pii | tenant-user |
| error | `cannot get %s/%s podInfo: %v` | pod namespace, pod name, error | tenant-pii | tenant-user |
| error | `cannot get namespace info: %v %v` | pod name, error | tenant-pii | tenant-user |
| error | `cannot parse peer address %q: %v` | peer pod address, parse error | customer-data | end-customer |
| error | `failed to apply rules for %s/%s: %v` | pod namespace, pod name, error (can repeat rule contents and addresses) | customer-data | end-customer |
| error | `failed to clean legacy daemon-owned tables for pod (%s/%s): %v` | pod namespace, pod name, error | tenant-pii | tenant-user |
| error | `failed to close CRI connection: %v` | error | none | none |
| error | `failed to close nftables for pod (%s/%s): %v` | pod namespace, pod name, error | tenant-pii | tenant-user |
| error | `failed to delete daemon-owned legacy rule %q from chain %q in table %q: %v` | rule comment, chain, table, error | tenant-pii | tenant-user |
| error | `failed to delete rule %q in chain %q: %v` | rule comment (policy name), chain, error | tenant-pii | tenant-user |
| error | `failed to delete unhashable rule %q in chain %q: %v` | rule comment (policy name), chain, error | tenant-pii | tenant-user |
| error | `failed to get pod info for %s/%s: %v` | pod namespace, pod name, error (can repeat network status with addresses) | customer-data | end-customer |
| error | `failed to get pod network annotation: %v` | error (can repeat the network-status annotation with addresses) | customer-data | end-customer |
| error | `failed to get pod(%s/%s) network status: %v` | pod namespace, pod name, error (can repeat addresses) | customer-data | end-customer |
| error | `Health server exited with error: %v` | error | none | none |
| error | `Health server shutdown error: %v` | error | none | none |
| error | `manager stopped during shutdown with error, running post-shutdown cleanup: %v` | error | none | none |
| error | `manager stopped with error, skipping post-shutdown cleanup: %v` | error | none | none |
| warning | `failed to get key for rule %q in chain %q: %v — deleting as stale` | rule comment (policy name), chain, error | tenant-pii | tenant-user |
| warning | `policy %s/%s has no %s annotation and is ignored` | policy namespace, policy name, annotation name | tenant-pii | tenant-user |
| warning | `policy %s: %s named port %q (%s) matches no container port and allows nothing` | policy namespace/name, direction, port name, protocol | tenant-pii | tenant-user |
| info | `cleanup: failed to connect to CRI: %v` | error | none | none |
| info | `cleanup: failed to list pods: %v` | error | none | none |
| info | `cleanup: FAILED to remove rules for %s/%s: %v` | pod namespace, pod name, error | tenant-pii | tenant-user |
| info | `cleanup: finished for node %s (total=%d targeted=%d)` | node name, counts | none | none |
| info | `cleanup: found %d pods on node %s` | count, node name | none | none |
| info | `cleanup: pod %s/%s: cannot resolve netns (err=%v), skipping` | pod namespace, pod name, error | tenant-pii | tenant-user |
| info | `cleanup: removing rules for %s/%s` | pod namespace, pod name | tenant-pii | tenant-user |
| info | `cleanup: starting nftables cleanup for node %s` | node name | none | none |
| info | `cleanup: SUCCESS removed rules for %s/%s` | pod namespace, pod name | tenant-pii | tenant-user |
| info | `Execute failed: %v` | error that ended the process: an invalid option, a start-up failure, or the errors of the shutdown cleanup, which name pods | tenant-pii | tenant-user |
| info | `flush-cleanup %s/%s: %d tables remaining after flush` | pod namespace, pod name, count | tenant-pii | tenant-user |
| info | `flush-cleanup %s/%s: DelTable %s (family=%d)` | pod namespace, pod name, table, family | tenant-pii | tenant-user |
| info | `flush-cleanup %s/%s: no managed tables found, nothing to clean` | pod namespace, pod name | tenant-pii | tenant-user |
| info | `Health server listening on %s` | listen address of the health server | none | none |
| info | `hostname: %v` | node name | none | none |
| info | `Manager stopped, running post-shutdown cleanup` |  | none | none |
| info | `No kubeconfig specified. Falling back to in-cluster config.` |  | none | none |
| info | `Starting manager for node %s` | node name | none | none |
| V(1) | `Pod:%s/%s %s/%s, not ready` | pod namespace, pod name, local node name, pod node name | tenant-pii | tenant-user |
| V(1) | `pod:%s/%s %s/%s` | pod namespace, pod name, local node name, pod node name | tenant-pii | tenant-user |
| V(2) | `preserving daemon-owned legacy chain %q in table %q because it still contains foreign rules` | chain, table | tenant-pii | tenant-user |
| V(2) | `preserving daemon-owned legacy chain %q in table %q because remaining rules still reference it` | chain, table | tenant-pii | tenant-user |
| V(2) | `removing daemon-owned legacy chain %q from table %q` | chain, table | tenant-pii | tenant-user |
| V(2) | `removing daemon-owned legacy rule %q from chain %q in table %q` | rule comment, chain, table | tenant-pii | tenant-user |
| V(2) | `removing daemon-owned legacy set %q from table %q` | set, table | tenant-pii | tenant-user |
| V(2) | `replacing constant set %q, table %q` | set, table | tenant-pii | tenant-user |
| V(4) | `Generate rules for Pod: [%s]` | pod namespace/name | tenant-pii | tenant-user |
| V(4) | `pod %s/%s has no relevant interfaces, skipping` | pod namespace, pod name | tenant-pii | tenant-user |
| V(6) | `netdef->pluginMap: %v` | NetworkAttachmentDefinition namespace/name to plugin type | tenant-pii | tenant-user |
| V(6) | `Pod: %s/%s netns:%s netIF:%v` | pod namespace, pod name, netns path, pod interfaces with IP addresses | customer-data | end-customer |
| V(8) | `%s/%s: net: %v` | pod namespace, pod name, network status with IP and MAC addresses | customer-data | end-customer |
| V(8) | `%s/%s: NO NET` | pod namespace, pod name | tenant-pii | tenant-user |
| V(8) | `adding chain %q` | chain (contains the policy namespace/name) | tenant-pii | tenant-user |
| V(8) | `adding rule comment:%q chain:%s` | rule comment, chain | tenant-pii | tenant-user |
| V(8) | `adding set %q, table %q` | set, table | tenant-pii | tenant-user |
| V(8) | `adding table %q` | table | none | none |
| V(8) | `applying common chain rules` |  | none | none |
| V(8) | `applying mark check %q: %s` | policy name, chain | tenant-pii | tenant-user |
| V(8) | `applying mark reset %q: %s` | policy name, chain | tenant-pii | tenant-user |
| V(8) | `applying peers rules with namespace selector only (all pods in matched namespaces)` |  | none | none |
| V(8) | `applying peers rules with namespace selector: %s` | namespace label selector | tenant-pii | tenant-user |
| V(8) | `applying peers rules with pod selector: %s` | pod label selector | tenant-pii | tenant-user |
| V(8) | `applying pod interface:%s [%q] policy %q chain: %s` | interface name, interface type, policy namespace/name, chain | tenant-pii | tenant-user |
| V(8) | `applying port rules for policy %q in the chain %q` | policy name, chain | tenant-pii | tenant-user |
| V(8) | `deleting chain %q in table %q` | chain, table | tenant-pii | tenant-user |
| V(8) | `deleting rule %q in chain %q` | rule comment, chain | tenant-pii | tenant-user |
| V(8) | `deleting set %q in table %q` | set, table | tenant-pii | tenant-user |
| V(8) | `forcing rule update comment:%q chain:%s` | rule comment, chain | tenant-pii | tenant-user |
| V(8) | `ingress/egress = %v/%v` | policy types | none | none |
| V(8) | `NetnsPath: %s` | netns path of a pod (contains the pod sandbox ID) | none | none |
| V(8) | `networkPlugins[%s], %v` | NetworkAttachmentDefinition namespace/name, plugin type | tenant-pii | tenant-user |
| V(8) | `updating set %q, table %q` | set, table | tenant-pii | tenant-user |
| V(10) | `found rule comment:%q chain:%s` | rule comment, chain | tenant-pii | tenant-user |
<!-- END MESSAGE CATALOG -->

## Keeping this document current

- A change that adds, removes or changes a log call updates the catalog in the same pull request, with the personal-data class and data subject of the new row. `TestLoggingReference` runs in `make test` (`./...`) in the `test` workflow and fails otherwise. It also fails for a call without a literal message or V level, and for a row without a valid class and data subject.
- Every klog function that writes a line (`Info*`, `Warning*`, `Error*`, `Fatal*`, `Exit*`, including the `Depth` and `S` forms) is scanned. A new function that wraps klog with a format argument is added to `wrappers` in `docs/logging_test.go`.
- The prose sections are not checked. A change of a flag, a default or a library that logs updates them in the same pull request.
- The scan matches calls on the package name `klog` and the wrappers only. It does not see a logger obtained from klog (`klog.Background()`, `klog.NewKlogr()`, a `logr.Logger`) or a direct write to stderr; a change that introduces one adds its messages to the catalog by hand and extends the scan.
