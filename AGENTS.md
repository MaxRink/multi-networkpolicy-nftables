<!-- SPDX-FileCopyrightText: 2026 Deutsche Telekom AG -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# Agent guidance

## Reuse upstream libraries before writing helpers

Humans and AI agents must check maintained upstream APIs before adding helper
code, even if a consumer adoption PR has not merged. Use this order:

1. Go standard library.
2. Kubernetes APIs: `k8s.io/apimachinery`, `k8s.io/client-go`, and
   `sigs.k8s.io/controller-runtime`.
3. Flux packages under `github.com/fluxcd/pkg`.
4. Other well-known, maintained libraries already compatible with this repo.
5. Relevant merged packages in
   [`telekom/t-caas-go-library`](https://github.com/telekom/t-caas-go-library/blob/main/docs/upstream-libraries.md);
   use the copied package notes below if you cannot access that repository.
6. Custom code only when no suitable upstream API fits the required semantics.

The library repository is public. The package notes below are a snapshot, not a
claim that this repository imports the library: it currently does not.
Check Go/Kubernetes version compatibility, licensing, dependency cost, and
behavioral differences before adopting a package. The paths below are specific
imports, not instructions to add every package as a dependency.

| Concern | Prefer this upstream API | Keep these project semantics local |
| --- | --- | --- |
| IP addresses and prefixes | `net/netip` | Address-family and input-validation policy |
| Prefix ranges and sets | `go4.org/netipx` | nftables interval encoding and boundary behavior |
| Kubernetes IP compatibility | `k8s.io/utils/net` | Avoid legacy conversions unless required |
| Netlink messages | `github.com/mdlayher/netlink` | nftables protocol and namespace behavior |
| nftables rules | `github.com/google/nftables` | Policy-to-rule translation |
| Event predicates | `sigs.k8s.io/controller-runtime/pkg/predicate` | Pod state and network-annotation comparisons |
| Event mapping | `sigs.k8s.io/controller-runtime/pkg/handler` | Node-scoped reconcile requests |
| Field indexes | `sigs.k8s.io/controller-runtime/pkg/client` (`FieldIndexer`) | Index keys and node scope |
| Cache readiness | `sigs.k8s.io/controller-runtime/pkg/cache` and `sigs.k8s.io/controller-runtime/pkg/healthz` | Which informers gate readiness |
| Manager lifecycle | `sigs.k8s.io/controller-runtime/pkg/manager` | Bounded cleanup and shutdown policy |
| Kubernetes polling | `k8s.io/apimachinery/pkg/util/wait` | Retry condition and timeout policy |
| API conflict retries | `k8s.io/client-go/util/retry` | Fresh reads and idempotent mutations |
| Work queues | `k8s.io/client-go/util/workqueue` | Queue keys and reconciliation policy |
| Envtest | `sigs.k8s.io/controller-runtime/pkg/envtest` | Pinned asset setup and suite lifecycle |
| Test assertions | `github.com/onsi/gomega` | Project-specific test predicates |
| Prometheus metrics | `github.com/prometheus/client_golang/prometheus` | Metric names, labels, and lifecycle |
| Checked IP arithmetic | `github.com/telekom/t-caas-go-library/pkg/netutil` | Use only for supported arithmetic/subdivision; not nftables set encoding |

The merged `pkg/netutil` may help if checked address arithmetic or budgeted
prefix subdivision is needed; this repo already uses `net/netip` and
`go4.org/netipx` for its current address and set operations. The library's
`pkg/patch`, `pkg/remoteclient`, and `pkg/namespaceselector` do not currently
match a demonstrated need here. Namespace selectors currently use cached
Kubernetes labels, not the live, request-local reads supplied by
`pkg/namespaceselector`; dynamic namespace cache filtering would omit peers
needed for cross-namespace network policy evaluation.

The library's root module at `v0.1.0` requires Go 1.26.6, Kubernetes modules
0.37.1, and controller-runtime 0.25.2. This repository uses Go 1.25.13 and
Kubernetes 0.35.1; its controller-runtime alignment targets 0.23.3. Importing
even a non-Kubernetes library package would raise the module graph's version
requirements. Do not force a stack upgrade merely to adopt a helper that
existing dependencies already supply.

Convenience wrappers are acceptable only when
the same glue repeats across multiple repositories; contribute that shared
glue to `telekom/t-caas-go-library` rather than duplicating it.

Prefer `predicate.NewPredicateFuncs` for constant node-name filtering; keep
the typed Pod state and network-annotation comparisons domain-specific.
`pkg/controller/indexes.go` and `pkg/controller/mappers.go` use native index
and mapping APIs. The informer readiness and bounded
shutdown cleanup in `cmd/multi-networkpolicy-nftables/main.go` also encode
local lifecycle policy; no shared wrapper is justified.
