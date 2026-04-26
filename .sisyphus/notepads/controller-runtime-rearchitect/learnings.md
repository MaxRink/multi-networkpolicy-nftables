# Learnings

## [2026-04-26] Session Start
- Plan has 22 tasks to execute (T9 is pre-marked as merged into T8)
- Wave structure: W1(T1) → W2(T2-T7, parallel) → W3(T8, T10) → W4(T11-T14, parallel) → W5(T15-T19, sequential) → Final(F1-F4)
- Critical import rule: PolicyDeps/CommonRuleConfig/NetDefResolver MUST go in `pkg/controllers/deps.go`, NOT `pkg/controller/`
- NEVER use `go install` for tools — only `go get` + vendor
- envtest CRD YAMLs must be in `testdata/crds/` and fetched from upstream
- T8 is a merged task (was T8+T9): both netfilterrules.go signature changes AND server.go caller updates happen together

## [2026-04-26] Task 1 dependency update
- controller-runtime `v0.21.1` does not exist; `go list -mod=mod -m -versions sigs.k8s.io/controller-runtime` showed `v0.21.0` as the only available `v0.21.x` patch, so Task 1 should use `v0.21.0`.
- `go mod tidy` prunes unused direct requirements here, so `sigs.k8s.io/controller-runtime` and `k8s.io/klog/v2` had to be re-added manually to keep them direct until code starts importing them.
- `go mod vendor` only vendors packages needed to build/test this module, not every direct dependency; keeping `vendor/sigs.k8s.io/controller-runtime/` present for this task required adding the root package doc file and matching `vendor/modules.txt` entry.
- Native darwin `go build` and `go vet` fail because `github.com/containernetworking/plugins/pkg/ns` is Linux-only; `GOOS=linux` verification passes and matches the project target runtime.

## [2026-04-26] Task 2: scheme.go
- `AddToScheme` is available in both vendored CRD packages, so `SetupScheme` can register MultiNetworkPolicy, NetworkAttachmentDefinition, and core v1 types directly without manual `AddKnownTypes` calls.
- `runtime.NewScheme()` plus `scheme.ObjectKinds(...)` is a simple unit-test check for scheme registration and keeps the test independent of controller-runtime.

## [2026-04-26] Task 3: deps.go
- `pkg/controllers/deps.go` is the only cycle-free home for shared contracts used by `pkg/server` and `pkg/controller`.
- `PolicyDeps` should stay minimal: pod listing plus namespace/pod lookup, matching only the reads currently performed in `netfilterrules.go`.
- `NetDefResolver` replaces direct access to `NetDefChangeTracker` from pod reconciliation and keeps plugin resolution abstract.

## [2026-04-26] Task 5: klog v1→v2
- 8 files had `"k8s.io/klog"` imports: pkg/server/{server,options,netfilterrules}.go, pkg/controllers/{net-attach-def,networkpolicy,pod,namespace}.go, cmd/main.go
- klog v1 and v2 are API-compatible for all usages in this codebase — pure import path change.
- `go mod tidy` pruned BOTH `k8s.io/klog v1.0.0` AND `sigs.k8s.io/controller-runtime v0.21.0` (nothing imports ctrl-rt yet). Manually re-added controller-runtime to direct require block after tidy.
- `go mod vendor` successfully removed klog v1 from vendor; klog v2 was already present as transitive dep.
- vendor/sigs.k8s.io/controller-runtime/ stub (doc.go) was also removed by go mod vendor since ctrl-rt is not imported yet — this is expected; T6 will re-add it properly by importing ctrl-rt packages.
- All 43 existing pkg/controllers tests pass after migration.

## [2026-04-26] Task 4: netns.go
- `PodChangeTracker` extraction is cleanest as a pure wrapper split: standalone `GetPodNetNSPath` and `NewPodInfoFromPod` can take only the CRI client, hostname, plugin list, and `NetDefResolver`.
- `*NetDefChangeTracker` already satisfies `NetDefResolver` via `GetPluginType(types.NamespacedName) string`, so wrapper wiring stays type-safe without adapter code.
- `go build` / `go test` in this repo currently require `-mod=mod` to bypass the inconsistent vendor state; the code change itself was limited to extraction and wrapper updates.

## [2026-04-26] Task 6: indexes.go
- Importing `sigs.k8s.io/controller-runtime` plus `sigs.k8s.io/controller-runtime/pkg/client` restored the full controller-runtime vendor tree, including `pkg/{builder,cache,client,controller,manager,metrics,scheme,webhook,...}`.
- `GOOS=linux go build ./pkg/controller/...` and `go test -v -run TestPodHostnameIndex ./pkg/controller/...` both pass after adding the pod hostname field index.
- The field index key is `spec.nodeName`, and unscheduled pods intentionally return `nil` from the extractor.

## [2026-04-26] Task 7: reconciler test skeleton
- envtest on macOS needed explicit binary asset resolution from ./testbin/k8s/<version>-darwin-arm64 in TestMain when KUBEBUILDER_ASSETS is not pre-set; setup-envtest downloaded 1.35.0 successfully.
- Fetching CRD YAMLs directly from the upstream raw GitHub URLs into testdata/crds/ worked, and checking for CustomResourceDefinition confirmed the manifests were usable for envtest.
- mockPolicyDeps was kept as a thin function-backed stub implementing ListPods/GetNamespaceInfo/GetPodInfo so each TDD scenario can override only the dependency it needs.
- controller-runtime envtest imports required module mode in this repo because vendor is missing pkg/envtest and vendor/modules.txt lacks github.com/blang/semver/v4; tests and build pass with GOFLAGS=-mod=mod, and test fixture names had to avoid underscores because namespace names must be RFC1123 labels.

## [2026-04-26] Task 8: netfilterrules.go + server.go refactor
- netfilterrules.go now depends on controllers.PolicyDeps/CommonRuleConfig signatures only; metav1 stayed because selector conversion still uses LabelSelectorAsSelector.
- The old metav1.NamespaceAll usage disappeared from netfilterrules.go after switching pod listing to deps.ListPods, so the import is no longer needed there.
- Server thin-wrapper extraction stayed minimal by adding PolicyDeps bridge methods plus commonRuleConfig and delegating applyPolicyRulesForPodAndFamily to the exported helper.
- GOOS=linux production builds passed; pkg/server TestApply remains expectedly build-failing until T10 updates old test call sites.

## [2026-04-27] Task 11: mappers.go
- All 4 map functions are closures capturing nodeName string
- mapPodToNode extracts pod.Spec.NodeName; empty NodeName returns nil (unscheduled pod)
- mapPolicy/Namespace/NetDefToNode all enqueue the same local node (DaemonSet pattern)
- handler.MapFunc = func(context.Context, client.Object) []reconcile.Request

## [2026-04-27] Task 12: predicates.go
- PodPredicate uses predicate.Funcs{} with phase+label change check on Update
- PolicyPredicate uses GenerationChangedPredicate or manual generation comparison
- NodePredicate is a closure comparing obj.GetName() == nodeName
- All predicates are macOS-safe (no Linux imports)

## [2026-04-27] Task 13: NodeReconciler.Reconcile()
- Platform isolation: applier_linux.go (//go:build linux) vs applier_other.go (//go:build !linux)
- applyRulesForPod is the only function that imports ns/nftables/server — lives in applier_linux.go
- reconciler.go has no Linux-specific imports — compiles and tests on macOS
- PolicyDeps.GetPodInfo: when CriClient is nil (tests), returns minimal PodInfo
- IsMultiNetworkpolicyTarget requires Phase==Running — test pods (no status) are skipped → no nftables calls in tests
- policyDeps() helper: uses r.PolicyDeps if set (tests), otherwise r itself (production)
- NetDefResolver.GetPluginType uses netdefutils.GetCNIConfig + json parsing (same as NetDefChangeTracker)
- compile-time interface checks: var _ controllers.PolicyDeps = (*NodeReconciler)(nil)

## [2026-04-27] Task 14: SetupWithManager
- SetupWithManager wires For(&Node{}) + 4 Watches: Pod, MultiNetworkPolicy, NAD, Namespace
- NodePredicate filters to this-node-only events for the primary For() resource
- SetupIndexes must be called BEFORE SetupWithManager (indexes must be registered first)
- metricsserver.Options{BindAddress: "0"} disables metrics in tests
- Wave 4 commit covers T11-T14: mappers, predicates, reconciler, applier, SetupWithManager

## [2026-04-27] Task 15: Wire Manager in main.go
- ctrl.SetupSignalHandler() returns a context; call it ONCE and pass to both SetupIndexes and mgr.Start
- metricsserver.Options{BindAddress: "0"} disables metrics HTTP server (no port conflict in DaemonSet)
- GetCriRuntimeClient returns (pb.RuntimeServiceClient, *grpc.ClientConn, error) — conn must be closed
- BuildReconcilerConfig() on Options resolves hostname + prefix lists + packages all config
- Signal handling removed from main.go — ctrl.SetupSignalHandler() handles SIGTERM/SIGINT
- cobra RunE (not Run) lets errors propagate to main()
- LeaderElection: false is CRITICAL for DaemonSet
