# Rearchitect multi-networkpolicy-nftables around controller-runtime

## TL;DR

> **Quick Summary**: Migrate from raw client-go informers + custom ChangeTrackers + BoundedFrequencyRunner to a controller-runtime Manager with a Node-anchored full-sweep Reconciler. Decouple nftables engine from Server struct. Migrate klog v1 → logr.
> 
> **Deliverables**:
> - controller-runtime Manager replacing manual informer wiring
> - NodeReconciler with watches on Pod/MultiNetworkPolicy/NAD/Namespace
> - PolicyDeps interface + NetDefResolver interface replacing *Server in nftables engine
> - CRD scheme registration for MultiNetworkPolicy + NetworkAttachmentDefinition
> - logr structured logging throughout
> - Removal of all ChangeTrackers, XxxConfig/XxxHandler types, BoundedFrequencyRunner, Server struct
> - Removal of k8s.io/kubernetes monorepo dependency
> - TDD test suite for new reconciler code
> 
> **Estimated Effort**: Large
> **Parallel Execution**: YES - 5 waves
> **Critical Path**: T1 → T3 → T8 → T13 → T15 → T17 → F1-F4

---

## Context

### Original Request
Rearchitect the multi-networkpolicy-nftables controller around using the controller-runtime client to better track reconciliations.

### Interview Summary
**Key Discussions**:
- **Reconciler Design**: Full-node sweep per event. Node object is the anchor — any change to Pod/Policy/NAD/Namespace enqueues the Node for reconciliation. Inside Reconcile(), sweep all local pods and recompute nftables.
- **Change Trackers**: Replace entirely with controller-runtime cache. Remove all 4 ChangeTracker types.
- **nftables Engine**: Decouple from *Server. Extract PolicyDeps interface and CommonRuleConfig struct.
- **Logging**: Migrate klog v1 → logr (structured logging). Mechanical translation, no message rewording.
- **Tests**: TDD — write reconciler tests first using envtest, then implement.

**Research Findings**:
- `openshift/multus-networkpolicy` (SHA: 43b16450b7) is a production controller-runtime implementation of the exact same domain (MultiNetworkPolicy → nftables). Uses `ctrl.NewControllerManagedBy`, `EnqueueRequestsFromMapFunc`, field indexers, custom predicates.
- Current codebase uses klog v1.0.0 (not v2) — requires v1→v2 step before logr migration.
- `k8s.io/kubernetes` monorepo is only imported for `BoundedFrequencyRunner` — can be fully removed.
- 4 functions in `netfilterrules.go` accept `*Server`: `applyCommonChainRules` (line 823), `applyPolicyPeersRulesSelector` (line 1216), `applyPolicyPeersRules` (line 1423), `applyPodRules`.
- Mid-loop state mutations in `applyPolicyPeersRulesSelector` (lines 1248, 1261) call `s.podMap.Update()` and `s.namespaceMap.Update()` — these become unnecessary with controller-runtime cache.

### Metis Review
**Identified Gaps** (addressed):
- klog v1→v2 prerequisite before logr migration — added as explicit task
- LeaderElection must be disabled for DaemonSet — added as guardrail
- k8s.io/kubernetes removal as explicit benefit — added as cleanup task
- Mid-loop state mutation semantics change with cache — documented in task acceptance criteria
- Vendor mode must be preserved — added as guardrail
- Graceful shutdown must clean up nftables — added as explicit task
- Test environment: nftables tests require Linux+root; new reconciler tests use envtest (no root needed)

---

## Work Objectives

### Core Objective
Replace the custom informer + ChangeTracker + BoundedFrequencyRunner reconciliation architecture with controller-runtime's Manager/Reconciler pattern, enabling per-reconciliation tracking, per-item rate limiting, and idiomatic Kubernetes controller behavior.

### Concrete Deliverables
- `pkg/controller/reconciler.go` — NodeReconciler implementation
- `pkg/controller/reconciler_test.go` — TDD test suite (envtest)
- `pkg/controller/mappers.go` — event-to-node map functions
- `pkg/controller/predicates.go` — event filter predicates
- `pkg/controller/indexes.go` — PodHostnameIndex field indexer
- `pkg/controller/deps.go` — PolicyDeps interface + CommonRuleConfig + NetDefResolver (in `pkg/controllers/`)
- Refactored `pkg/server/netfilterrules.go` — no *Server dependency
- Refactored `cmd/multi-networkpolicy-nftables/main.go` — controller-runtime Manager
- Slimmed `pkg/controllers/` — types only, no Config/Handler/ChangeTracker

### Definition of Done
- [ ] All e2e Bats tests pass: `cd e2e && ./run_all_tests.sh` — zero failures
- [ ] All unit tests pass: `sudo go test -v -count=1 ./...` — zero failures
- [ ] `golangci-lint run` — zero new violations
- [ ] `go build ./cmd/multi-networkpolicy-nftables/` succeeds
- [ ] `docker build .` succeeds
- [ ] `grep -rn "k8s.io/kubernetes" pkg/ cmd/ --include="*.go"` returns empty
- [ ] `grep -n "ChangeTracker\|BoundedFrequencyRunner" pkg/ cmd/ --include="*.go" | grep -v _test.go` returns empty

### Must Have
- controller-runtime Manager with LeaderElection disabled
- Node-anchored Reconciler triggered by Pod/Policy/NAD/Namespace changes
- CRD scheme registration for MultiNetworkPolicy (v1beta1) and NetworkAttachmentDefinition (v1)
- PolicyDeps interface decoupling nftables engine from Server
- Field indexer for node-local pod queries
- Graceful shutdown with nftables cleanup
- logr structured logging

### Must NOT Have (Guardrails)
- DO NOT modify nftables rule generation logic (chain structure, set management, rule expressions in netfilterrules.go)
- DO NOT modify CRI pod netns resolution logic (getPodNetNSPath, GetCriRuntimeClient)
- DO NOT add leader election (DaemonSet — each pod owns its node)
- DO NOT add custom Prometheus metrics beyond controller-runtime defaults
- DO NOT add custom health/readiness probes beyond controller-runtime defaults
- DO NOT migrate MultiNetworkPolicy v1beta1 → v1beta2
- DO NOT change e2e tests
- DO allow adding `nodes: [get, list, watch]` to deploy.yml RBAC (required for controller-runtime Node watch)
- DO NOT "improve" log messages during klog→logr migration (mechanical 1:1 translation only)
- DO NOT remove vendor directory — add controller-runtime deps to vendor
- DO NOT refactor nftables internals (splitting files, renaming methods, reorganizing functions)
- DO NOT add tests for existing untested code — only test NEW code

---

## Verification Strategy

> **ZERO HUMAN INTERVENTION** - ALL verification is agent-executed. No exceptions.

### Test Decision
- **Infrastructure exists**: YES (Ginkgo/Gomega + Bats e2e)
- **Automated tests**: TDD for new code, update existing tests for refactored interfaces
- **Framework**: Ginkgo/Gomega for unit tests, envtest for reconciler tests, Bats for e2e
- **If TDD**: Each reconciler task follows RED (failing test) → GREEN (minimal impl) → REFACTOR

### QA Policy
Every task MUST include agent-executed QA scenarios.
Evidence saved to `.sisyphus/evidence/task-{N}-{scenario-slug}.{ext}`.

- **Go compilation**: `go build ./cmd/multi-networkpolicy-nftables/`
- **Unit tests**: `sudo go test -v -count=1 ./pkg/...` (nftables tests require Linux+root)
- **Lint**: `golangci-lint run`
- **Reconciler tests**: `KUBEBUILDER_ASSETS=$(setup-envtest use -p path) go test -v ./pkg/controller/...` (envtest, no root needed; requires `setup-envtest` binary and testbin/ assets from T7)

---

## Execution Strategy

### Parallel Execution Waves

```
Wave 1 (Blocking Foundation - 1 task):
└── Task 1: Add controller-runtime dependency + validate compatibility [deep]

Wave 2 (Foundation - 6 parallel tasks, after Wave 1):
├── Task 2: Register CRD types in runtime scheme [quick]
├── Task 3: Define PolicyDeps interface + CommonRuleConfig [quick]
├── Task 4: Extract CRI netns helper to standalone package [quick]
├── Task 5: Migrate klog v1 → v2 [unspecified-high]
├── Task 6: Implement PodHostnameIndex field indexer [quick]
└── Task 7: Write NodeReconciler TDD test skeleton [deep]

Wave 3 (Interface Extraction - 2 tasks, after Wave 2):
├── Task 8: Refactor netfilterrules.go + extract applyPolicyRulesForPodAndFamily (merged T8+T9) [deep]
└── Task 10: Update existing unit tests for refactored interfaces [unspecified-high]

Wave 4 (Reconciler Implementation - 4 parallel tasks, after Wave 3):
├── Task 11: Implement event map functions (mapToNode) [quick]
├── Task 12: Implement predicates for watched resources [quick]
├── Task 13: Implement NodeReconciler.Reconcile() core logic [deep]
└── Task 14: Implement NodeReconciler.SetupWithManager() [quick]

Wave 5 (Integration + Cleanup - 5 tasks, after Wave 4):
├── Task 15: Wire controller-runtime Manager in cmd/main.go [deep]
├── Task 16: Implement graceful shutdown (nftables cleanup) [unspecified-high]
├── Task 17: Remove old code (ChangeTrackers, Configs, Handlers, Server, Runner) [unspecified-high]
├── Task 18: Remove k8s.io/kubernetes dep + go mod tidy + vendor [quick]
└── Task 19: Migrate klog v2 → logr (mechanical translation) [unspecified-high]

Wave FINAL (After ALL tasks — 4 parallel reviews, then user okay):
├── Task F1: Plan compliance audit (oracle)
├── Task F2: Code quality review (unspecified-high)
├── Task F3: Real manual QA (unspecified-high)
└── Task F4: Scope fidelity check (deep)
-> Present results -> Get explicit user okay

Critical Path: T1 → T3 → T8 → T13 → T15 → T17 → F1-F4
Parallel Speedup: ~65% faster than sequential
Max Concurrent: 6 (Wave 2)
```

### Dependency Matrix

| Task | Depends On | Blocks | Wave |
|------|-----------|--------|------|
| T1 | - | T2-T7 | 1 |
| T2 | T1 | T7, T14 | 2 |
| T3 | T1 | T8 | 2 |
| T4 | T1 | T8, T13 | 2 |
| T5 | T1 | T19 | 2 |
| T6 | T1 | T13 | 2 |
| T7 | T1, T2, T3 | T13, T14 | 2 |
| T8 | T3, T4 | T10, T13 | 3 |
| T9 | - (merged into T8) | - | - |
| T10 | T8 | T13 | 3 |
| T11 | T1 | T14 | 4 |
| T12 | T1 | T14 | 4 |
| T13 | T6, T7, T8, T10 | T15 | 4 |
| T14 | T2, T7, T11, T12, T13 | T15 | 4 |
| T15 | T13, T14 | T16, T17 | 5 |
| T16 | T15 | T17 | 5 |
| T17 | T15, T16 | T18 | 5 |
| T18 | T17 | T19 | 5 |
| T19 | T5, T18 | F1-F4 | 5 |

### Agent Dispatch Summary

- **Wave 1**: **1** — T1 → `deep`
- **Wave 2**: **6** — T2 → `quick`, T3 → `quick`, T4 → `quick`, T5 → `unspecified-high`, T6 → `quick`, T7 → `deep`
- **Wave 3**: **2** — T8 → `deep` (merged T8+T9), T10 → `unspecified-high`
- **Wave 4**: **4** — T11 → `quick`, T12 → `quick`, T13 → `deep`, T14 → `quick`
- **Wave 5**: **5** — T15 → `deep`, T16 → `unspecified-high`, T17 → `unspecified-high`, T18 → `quick`, T19 → `unspecified-high`
- **FINAL**: **4** — F1 → `oracle`, F2 → `unspecified-high`, F3 → `unspecified-high`, F4 → `deep`

---

## TODOs

> Implementation + Test = ONE Task. Never separate.
> EVERY task MUST have: Recommended Agent Profile + Parallelization info + QA Scenarios.

- [x] 1. Add controller-runtime dependency + validate compatibility

  **What to do**:
  - Run `go get sigs.k8s.io/controller-runtime@latest` to add the controller-runtime dependency
  - Run `go get k8s.io/klog/v2@latest` to add klog v2 (prerequisite for logr migration)
  - Run `go mod tidy` to resolve dependency tree
  - Run `go mod vendor` to populate vendor directory
  - Verify `go build ./cmd/multi-networkpolicy-nftables/` still compiles
  - Verify `go test ./...` still passes (existing tests unaffected)
  - Check `go.mod` for any incompatible version conflicts between controller-runtime's transitive deps and existing deps (k8s.io/api, k8s.io/apimachinery, k8s.io/client-go must be at compatible versions)

  **Must NOT do**:
  - DO NOT modify any Go source files — this task is dependency management only
  - DO NOT remove k8s.io/kubernetes yet (that's Task 18)
  - DO NOT upgrade existing k8s.io/* deps beyond what controller-runtime requires

  **Recommended Agent Profile**:
  - **Category**: `deep`
    - Reason: Dependency resolution in a vendored project with k8s.io/kubernetes can be complex; requires problem-solving if version conflicts arise
  - **Skills**: []
  - **Skills Evaluated but Omitted**:
    - `git-master`: No git operations needed beyond final commit

  **Parallelization**:
  - **Can Run In Parallel**: NO
  - **Parallel Group**: Wave 1 (solo)
  - **Blocks**: T2, T3, T4, T5, T6, T7
  - **Blocked By**: None (can start immediately)

  **References**:

  **Pattern References**:
  - `go.mod:1-29` — Current direct dependencies; controller-runtime must be compatible with k8s.io/api v0.35.1, k8s.io/apimachinery v0.35.1, k8s.io/client-go v1.5.2
  - `go.mod:27` — `k8s.io/klog v1.0.0` — current klog v1 dep; v2 will be added alongside for now

  **External References**:
  - `sigs.k8s.io/controller-runtime` go.mod — check which k8s.io/* versions it requires
  - `k8s.io/klog/v2` — v2 module path (separate from v1, can coexist temporarily)

  **WHY Each Reference Matters**:
  - `go.mod` current deps: Need to detect version conflicts before they cascade into build failures
  - klog v2: Required intermediate step before logr migration; v1 and v2 can coexist in go.mod but must not conflict

  **Acceptance Criteria**:

  **QA Scenarios (MANDATORY):**

  ```
  Scenario: Dependency added and project builds
    Tool: Bash
    Preconditions: Clean working tree, vendor/ exists
    Steps:
      1. Run `grep "sigs.k8s.io/controller-runtime" go.mod` — verify dependency line present
      2. Run `grep "k8s.io/klog/v2" go.mod` — verify klog v2 dependency present
      3. Run `ls vendor/sigs.k8s.io/controller-runtime/` — verify vendored
      4. Run `go build ./cmd/multi-networkpolicy-nftables/` — must succeed with exit code 0
      5. Run `go vet ./...` — must succeed
    Expected Result: All commands exit 0; controller-runtime and klog v2 in go.mod and vendor/
    Failure Indicators: Build error, missing vendor dir, version conflict in go.mod
    Evidence: .sisyphus/evidence/task-1-deps-build.txt

  Scenario: Existing tests still pass
    Tool: Bash
    Preconditions: Dependencies added, build succeeds
    Steps:
      1. Run `sudo go test -v -count=1 ./pkg/server/... 2>&1 | tail -20` — check test results
      2. Run `sudo go test -v -count=1 ./pkg/controllers/... 2>&1 | tail -20` — check test results
    Expected Result: All existing tests pass with no new failures
    Failure Indicators: Any test failure not present before dependency addition
    Evidence: .sisyphus/evidence/task-1-tests-pass.txt
  ```

  **Commit**: YES
  - Message: `build(deps): add controller-runtime and klog v2 dependencies`
  - Files: `go.mod`, `go.sum`, `vendor/`
  - Pre-commit: `go build ./cmd/multi-networkpolicy-nftables/`

- [x] 2. Register CRD types in runtime scheme

  **What to do**:
  - Create file `pkg/controller/scheme.go` (new `controller` package)
  - Import `multiv1beta1 "github.com/k8snetworkplumbingwg/multi-networkpolicy/pkg/apis/k8s.cni.cncf.io/v1beta1"` and `netdefv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"`
  - Create a function `SetupScheme(scheme *runtime.Scheme) error` that registers both CRD types plus core k8s types (`k8s.io/api/core/v1`)
  - Use `multiv1beta1.AddToScheme(scheme)` and `netdefv1.AddToScheme(scheme)` (check if these exist in the CRD client libraries — if not, use `scheme.AddKnownTypes`)
  - Write a unit test in `pkg/controller/scheme_test.go` verifying that after `SetupScheme`, the scheme can create objects of type `*multiv1beta1.MultiNetworkPolicy` and `*netdefv1.NetworkAttachmentDefinition`

  **Must NOT do**:
  - DO NOT register types that aren't used (e.g., no RBAC types, no apps/v1)
  - DO NOT create a global scheme variable — pass scheme as parameter

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: Small file creation with well-defined pattern
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 2 (with T3, T4, T5, T6, T7)
  - **Blocks**: T7, T14
  - **Blocked By**: T1

  **References**:

  **Pattern References**:
  - `pkg/server/server.go:31-37` — Current CRD type imports showing exact import paths for MultiNetworkPolicy and NetworkAttachmentDefinition
  - `pkg/server/server.go:49` — `"k8s.io/client-go/kubernetes/scheme"` — existing scheme usage pattern

  **API/Type References**:
  - `github.com/k8snetworkplumbingwg/multi-networkpolicy/pkg/apis/k8s.cni.cncf.io/v1beta1` — MultiNetworkPolicy CRD types
  - `github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1` — NetworkAttachmentDefinition CRD types

  **External References**:
  - openshift/multus-networkpolicy SHA 43b16450b7 — check how they register CRD schemes with controller-runtime Manager

  **WHY Each Reference Matters**:
  - `server.go:31-37`: Exact import paths to use — don't guess, copy from working code
  - CRD client libraries: Must check if `AddToScheme` functions exist; if not, manual registration needed

  **Acceptance Criteria**:

  **QA Scenarios (MANDATORY):**

  ```
  Scenario: Scheme registration succeeds
    Tool: Bash
    Preconditions: T1 complete, controller-runtime vendored
    Steps:
      1. Run `go test -v -run TestSetupScheme ./pkg/controller/...` — test must pass
      2. Run `go build ./cmd/multi-networkpolicy-nftables/` — build must succeed
    Expected Result: Test passes confirming MultiNetworkPolicy and NetworkAttachmentDefinition types registered
    Failure Indicators: "not registered" error, import path mismatch, AddToScheme not found
    Evidence: .sisyphus/evidence/task-2-scheme-test.txt

  Scenario: Scheme scope is correct (no over-registration)
    Tool: Bash
    Preconditions: Scheme test exists
    Steps:
      1. Run `grep -c "scheme.New\|AddToScheme\|SchemeBuilder" pkg/controller/scheme.go` — count registration calls
      2. Run `grep "AddToScheme" pkg/controller/scheme.go` — should only show MultiNetworkPolicy, NetworkAttachmentDefinition, and core v1 registrations
      3. Run `go test -v -run TestSetupScheme ./pkg/controller/... 2>&1 | grep -E "PASS|FAIL"` — test must PASS
    Expected Result: Only 3 scheme registrations (MultiNetworkPolicy, NAD, core v1), no extra types
    Failure Indicators: Extra AddToScheme calls for unrelated types, test failure
    Evidence: .sisyphus/evidence/task-2-scheme-scope.txt
  ```

  **Commit**: NO (groups with Wave 2)

- [x] 3. Define PolicyDeps interface + CommonRuleConfig + NetDefResolver

  **What to do**:
  - Create file `pkg/controllers/deps.go` (**CRITICAL**: in `pkg/controllers/`, NOT `pkg/controller/` — this avoids an import cycle. `pkg/server` already imports `pkg/controllers`, and `pkg/controller` will also import `pkg/controllers`. Both are one-way dependencies. If this were in `pkg/controller`, then `pkg/server/netfilterrules.go` would import `pkg/controller` while `pkg/controller/reconciler.go` imports `pkg/server` → cycle.)
  - Define `PolicyDeps` interface with exactly these methods (derived from `*Server` field access in netfilterrules.go):
    ```go
    type PolicyDeps interface {
        ListPods(selector labels.Selector) ([]*v1.Pod, error)
        GetNamespaceInfo(namespace string) (*NamespaceInfo, error)
        GetPodInfo(pod *v1.Pod) (*PodInfo, error)
    }
    ```
    Note: `NamespaceInfo` and `PodInfo` are in the SAME package (`pkg/controllers`), so no import needed — use unqualified names.
  - Define `CommonRuleConfig` struct with exactly these fields (derived from `s.Options.*` access in `applyCommonChainRules`):
    ```go
    type CommonRuleConfig struct {
        AcceptICMPv6   bool
        AcceptICMP     bool
        AllowSrcPrefix []string
        AllowDstPrefix []string
    }
    ```
  - Define `NetDefResolver` interface to replace `*NetDefChangeTracker` for NAD plugin type lookups:
    ```go
    type NetDefResolver interface {
        GetPluginType(namespacedName types.NamespacedName) string
    }
    ```
    This is needed because `newPodInfo` (pod.go:367-381) calls `pct.netdefChanges.GetPluginType(namespacedName)` to determine if a network attachment uses a plugin in the configured `networkPlugins` list. The Reconciler's implementation will read NAD objects from the controller-runtime cache to resolve plugin types.
  - Write unit test in `pkg/controllers/deps_test.go` verifying that the interfaces are satisfiable (create mock structs implementing them)
  - Note: The actual implementations of PolicyDeps and NetDefResolver will be in the NodeReconciler (Task 13). This task only defines the contracts.

  **Must NOT do**:
  - DO NOT implement PolicyDeps or NetDefResolver yet — only define the interfaces
  - DO NOT put this in `pkg/controller/` — MUST be in `pkg/controllers/` to avoid import cycle
  - DO NOT add methods beyond what netfilterrules.go and pod.go actually use
  - DO NOT add logging, metrics, or other concerns to the interfaces

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: Interface definition with clear requirements
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 2 (with T2, T4, T5, T6, T7)
  - **Blocks**: T4, T8, T9
  - **Blocked By**: T1

  **References**:

  **Pattern References**:
  - `pkg/server/netfilterrules.go:823-868` — `applyCommonChainRules(s *Server)` — accesses `s.Options.acceptICMPv6`, `s.Options.acceptICMP`, `s.Options.allowSrcPrefix`, `s.Options.allowDstPrefix` → these become `CommonRuleConfig` fields
  - `pkg/server/netfilterrules.go:1234` — `s.podLister.Pods(metav1.NamespaceAll).List(podSelector)` → becomes `PolicyDeps.ListPods(selector)`
  - `pkg/server/netfilterrules.go:1253` — `s.namespaceMap.GetNamespaceInfo(sPod.Namespace)` → becomes `PolicyDeps.GetNamespaceInfo(namespace)`
  - `pkg/server/netfilterrules.go:1262` — `s.podMap.GetPodInfo(sPod)` → becomes `PolicyDeps.GetPodInfo(pod)`
  - `pkg/server/netfilterrules.go:1248,1261` — `s.namespaceMap.Update(s.nsChanges)` and `s.podMap.Update(s.podChanges)` — these mid-loop mutations become NOPs with controller-runtime cache (cache is always current)
  - `pkg/controllers/pod.go:367-381` — `pct.netdefChanges.GetPluginType(namespacedName)` — the NAD lookup that `NetDefResolver.GetPluginType()` replaces

  **API/Type References**:
  - `pkg/controllers/namespace.go` — `NamespaceInfo` struct definition (same package — use unqualified)
  - `pkg/controllers/pod.go` — `PodInfo` struct definition (same package — use unqualified)
  - `k8s.io/apimachinery/pkg/labels` — `Selector` interface used in `ListPods`
  - `k8s.io/apimachinery/pkg/types` — `NamespacedName` used in `NetDefResolver.GetPluginType`

  **WHY Each Reference Matters**:
  - netfilterrules.go L823-868: Exact fields accessed → CommonRuleConfig field list
  - netfilterrules.go L1234,1253,1262: Exact methods called on Server → PolicyDeps method signatures
  - netfilterrules.go L1248,1261: Mid-loop mutations that disappear in the new design
  - pod.go L367-381: NAD plugin resolution → NetDefResolver interface design
  - **Package location rationale**: `pkg/controllers/` avoids import cycle: `pkg/server` imports `pkg/controllers` ✓, `pkg/controller` imports `pkg/controllers` ✓, `pkg/controller` imports `pkg/server` ✓ — all one-way

  **Acceptance Criteria**:

  **QA Scenarios (MANDATORY):**

  ```
  Scenario: Interfaces and struct compile and are testable
    Tool: Bash
    Preconditions: T1 complete
    Steps:
      1. Run `go build ./pkg/controllers/...` — must compile
      2. Run `go test -v -run TestPolicyDeps ./pkg/controllers/...` — mock implementation test passes
      3. Run `grep -c "PolicyDeps\|NetDefResolver\|CommonRuleConfig" pkg/controllers/deps.go` — should show all 3 types
    Expected Result: All interfaces/structs in pkg/controllers/deps.go, compile, mock test passes
    Failure Indicators: Wrong package, import cycle, missing type
    Evidence: .sisyphus/evidence/task-3-deps-interface.txt

  Scenario: No import cycle introduced
    Tool: Bash
    Preconditions: deps.go in pkg/controllers/
    Steps:
      1. Run `go build ./pkg/server/...` — must compile (imports pkg/controllers)
      2. Run `go build ./pkg/controllers/...` — must compile (no imports of pkg/server or pkg/controller)
      3. Run `grep -n "pkg/server\|pkg/controller" pkg/controllers/deps.go` — must return empty (no reverse imports)
    Expected Result: No import cycle — pkg/controllers has no imports of pkg/server or pkg/controller
    Failure Indicators: Import cycle compilation error
    Evidence: .sisyphus/evidence/task-3-no-cycle.txt
  ```

  **Commit**: NO (groups with Wave 2)

- [x] 4. Extract CRI netns helper to standalone package

  **What to do**:
  - Create file `pkg/controllers/netns.go` (same package to avoid import cycles)
  - Extract `getPodNetNSPath` logic from `PodChangeTracker` method into a standalone function: `func GetPodNetNSPath(criClient pb.RuntimeServiceClient, pod *v1.Pod) (string, error)`
  - Extract `newPodInfo` logic into a standalone function: `func NewPodInfoFromPod(pod *v1.Pod, criClient pb.RuntimeServiceClient, hostname string, networkPlugins []string, netdefResolver NetDefResolver) *PodInfo` — note this takes `NetDefResolver` (from T3) instead of `*NetDefChangeTracker`. The function reads NAD plugin type via `netdefResolver.GetPluginType(namespacedName)` instead of `pct.netdefChanges.GetPluginType(namespacedName)`. This is the key decoupling point from ChangeTracker-based NAD resolution to cache-based resolution.
  - Keep the original `pct.getPodNetNSPath` and `pct.newPodInfo` as thin wrappers calling the new standalone functions (to avoid breaking existing code until Task 17 removes PodChangeTracker)
  - Write unit test in `pkg/controllers/netns_test.go` verifying the standalone function compiles and has correct signature

  **Must NOT do**:
  - DO NOT remove PodChangeTracker or modify its external API
  - DO NOT change CRI gRPC logic (container ID parsing, PID resolution)
  - DO NOT modify test files other than adding new tests

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: Mechanical extraction with clear before/after pattern
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 2 (with T2, T3, T5, T6, T7)
  - **Blocks**: T9, T13
  - **Blocked By**: T1

  **References**:

  **Pattern References**:
  - `pkg/controllers/pod.go:271-323` — `getPodNetNSPath` method on PodChangeTracker — uses `pct.criClient`, `pct.criConn`. Extract `criClient` as parameter.
  - `pkg/controllers/pod.go:337-415` — `newPodInfo` method on PodChangeTracker — uses `pct.hostname`, `pct.networkPlugins`, `pct.netdefChanges`, calls `pct.getPodNetNSPath`. Extract all as parameters.
  - `pkg/controllers/pod.go:442-457` — `NewPodChangeTrackerCri` — shows how `criClient` is created and stored
  - `pkg/controllers/pod.go:596` — `GetCriRuntimeClient` — standalone factory function (already extracted, reuse)

  **API/Type References**:
  - `pkg/controllers/pod.go:180-264` — `PodChangeTracker` struct definition — shows fields to extract as function params
  - `k8s.io/cri-api/pkg/apis/runtime/v1` — `pb.RuntimeServiceClient` interface used for CRI calls

  **WHY Each Reference Matters**:
  - L271-323: The exact code to extract — executor must understand which `pct.*` fields become function params
  - L337-415: `newPodInfo` uses `pct.getPodNetNSPath`, `pct.hostname`, `pct.networkPlugins`, `pct.netdefChanges` — all become params
  - L596: `GetCriRuntimeClient` is already standalone — no need to extract, just reuse in Reconciler construction

  **Acceptance Criteria**:

  **QA Scenarios (MANDATORY):**

  ```
  Scenario: Standalone functions compile and PodChangeTracker still works
    Tool: Bash
    Preconditions: T1 complete
    Steps:
      1. Run `grep "func GetPodNetNSPath" pkg/controllers/netns.go` — verify function exists
      2. Run `grep "func NewPodInfoFromPod" pkg/controllers/netns.go` — verify function exists
      3. Run `go build ./pkg/controllers/...` — must compile
      4. Run `go build ./cmd/multi-networkpolicy-nftables/` — full binary must compile
    Expected Result: New standalone functions exist, old code unchanged, everything compiles
    Failure Indicators: Import cycle, missing parameter, compilation error
    Evidence: .sisyphus/evidence/task-4-cri-extract.txt

  Scenario: Existing PodChangeTracker tests still pass
    Tool: Bash
    Preconditions: Extraction complete
    Steps:
      1. Run `sudo go test -v -count=1 ./pkg/controllers/... 2>&1 | tail -30` — all existing tests must pass
    Expected Result: Zero test failures — thin wrappers preserve behavior
    Failure Indicators: Any existing test failure
    Evidence: .sisyphus/evidence/task-4-existing-tests.txt
  ```

  **Commit**: NO (groups with Wave 2)

- [x] 5. Migrate klog v1 → v2

  **What to do**:
  - Replace ALL `"k8s.io/klog"` imports with `"k8s.io/klog/v2"` across all .go files in `pkg/` and `cmd/`
  - Update klog API calls for v1→v2 breaking changes:
    - `klog.InitFlags(nil)` → `klog.InitFlags(nil)` (same API in v2)
    - `klog.Exit(err)` → `klog.Exit(err)` (same in v2)
    - `klog.V(N).Infof(...)` → same syntax in v2
    - `klog.Infof(...)`, `klog.Errorf(...)`, `klog.Warningf(...)` → same in v2
  - This is a mechanical find-and-replace: change the import path only
  - Remove `k8s.io/klog` v1 from `go.mod` (it should become unused after replacement)
  - Run `go mod tidy && go mod vendor`
  - Verify build and all tests pass

  **Must NOT do**:
  - DO NOT change log message text, verbosity levels, or format
  - DO NOT migrate to logr yet (that's Task 19)
  - DO NOT add structured logging fields — keep the klog.Infof/Errorf format strings as-is
  - DO NOT change klog.Flush or KlogWriter patterns in main.go

  **Recommended Agent Profile**:
  - **Category**: `unspecified-high`
    - Reason: Touches many files but each change is mechanical; needs thorough verification
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 2 (with T2, T3, T4, T6, T7)
  - **Blocks**: T19
  - **Blocked By**: T1

  **References**:

  **Pattern References**:
  - `cmd/multi-networkpolicy-nftables/main.go:37` — `"k8s.io/klog"` import
  - `pkg/server/server.go:57` — `"k8s.io/klog"` import
  - `pkg/server/options.go:28` — `"k8s.io/klog"` import
  - `pkg/controllers/pod.go:44` — `"k8s.io/klog"` import
  - `go.mod:27` — `k8s.io/klog v1.0.0` direct dependency to remove

  **External References**:
  - klog v2 migration guide: https://github.com/kubernetes/klog/blob/main/MIGRATION.md — Check for any v1→v2 API changes

  **WHY Each Reference Matters**:
  - Import paths: Every file with `"k8s.io/klog"` must be changed — missing one causes build failure
  - go.mod: Must remove v1 dep and ensure v2 is the only klog version

  **Acceptance Criteria**:

  **QA Scenarios (MANDATORY):**

  ```
  Scenario: All klog v1 imports replaced
    Tool: Bash
    Preconditions: T1 complete (klog v2 in go.mod)
    Steps:
      1. Run `grep -rn '"k8s.io/klog"' pkg/ cmd/ --include="*.go"` — must return empty (no v1 imports)
      2. Run `grep -rn '"k8s.io/klog/v2"' pkg/ cmd/ --include="*.go" | wc -l` — should be > 0
      3. Run `grep "k8s.io/klog " go.mod` — v1 must NOT be in go.mod (note trailing space to avoid matching v2)
      4. Run `go build ./cmd/multi-networkpolicy-nftables/` — must succeed
    Expected Result: Zero v1 imports remain, all replaced with v2, project builds
    Failure Indicators: Any remaining v1 import, build failure due to API change
    Evidence: .sisyphus/evidence/task-5-klog-migration.txt

  Scenario: Tests pass after klog migration
    Tool: Bash
    Preconditions: All imports replaced
    Steps:
      1. Run `sudo go test -v -count=1 ./... 2>&1 | tail -30` — all tests pass
    Expected Result: Zero test failures — klog v2 is API-compatible for our usage
    Failure Indicators: Any test failure caused by klog behavior change
    Evidence: .sisyphus/evidence/task-5-klog-tests.txt
  ```

  **Commit**: NO (groups with Wave 2)

- [x] 6. Implement PodHostnameIndex field indexer

  **What to do**:
  - Create file `pkg/controller/indexes.go`
  - Define a constant `PodHostnameIndex = "spec.nodeName"` (field path used as index key)
  - Implement function `SetupIndexes(ctx context.Context, mgr ctrl.Manager) error` that registers a field indexer on Pod objects:
    ```go
    mgr.GetFieldIndexer().IndexField(ctx, &v1.Pod{}, PodHostnameIndex,
        func(obj client.Object) []string {
            pod := obj.(*v1.Pod)
            if pod.Spec.NodeName == "" {
                return nil
            }
            return []string{pod.Spec.NodeName}
        })
    ```
  - Write unit test in `pkg/controller/indexes_test.go` using envtest or a mock indexer to verify the indexer extracts `spec.nodeName` correctly

  **Must NOT do**:
  - DO NOT add indexes for Namespace, MultiNetworkPolicy, or NAD — only Pod needs node-level filtering
  - DO NOT use custom index keys — use the standard `spec.nodeName` path

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: Small well-defined function following established controller-runtime pattern
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 2 (with T2, T3, T4, T5, T7)
  - **Blocks**: T13
  - **Blocked By**: T1

  **References**:

  **Pattern References**:
  - `pkg/server/server.go:479` — `multiutils.CheckNodeNameIdentical(s.Hostname, p.Spec.NodeName)` — the current node-local filtering that the field indexer replaces (filtering pods by node in `syncMultiPolicy`)

  **External References**:
  - openshift/multus-networkpolicy SHA 43b16450b7 — Uses `PodHostnameIndex` field indexer for efficient node-local pod queries; reference their exact implementation pattern
  - controller-runtime `FieldIndexer` API: `mgr.GetFieldIndexer().IndexField(ctx, obj, field, extractFunc)`

  **WHY Each Reference Matters**:
  - `server.go:479`: Shows the current manual node filtering in the sync loop — the indexer makes this a server-side query (`client.MatchingFields{PodHostnameIndex: nodeName}`) instead of client-side filtering
  - openshift reference: Production-proven pattern for the exact same use case

  **Acceptance Criteria**:

  **QA Scenarios (MANDATORY):**

  ```
  Scenario: Indexer function extracts nodeName correctly
    Tool: Bash
    Preconditions: T1 complete
    Steps:
      1. Run `go test -v -run TestPodHostnameIndex ./pkg/controller/...` — test must pass
      2. Run `grep "PodHostnameIndex" pkg/controller/indexes.go` — constant must exist
      3. Run `grep "IndexField" pkg/controller/indexes.go` — registration call must exist
    Expected Result: Test verifies indexer returns `[]string{nodeName}` for pods with NodeName set, nil for unscheduled pods
    Failure Indicators: Wrong field path, missing nil check for empty NodeName
    Evidence: .sisyphus/evidence/task-6-indexer.txt

  Scenario: Indexer handles edge cases
    Tool: Bash
    Preconditions: Test exists
    Steps:
      1. Run `go test -v -run TestPodHostnameIndex ./pkg/controller/... 2>&1` — full test output
      2. Run `grep -c "NodeName.*empty\|NodeName.*\"\"\|unscheduled\|no.*spec\|nil" pkg/controller/indexes_test.go` — should be >= 2 (edge case tests exist)
      3. Run `go test -v -run TestPodHostnameIndex ./pkg/controller/... 2>&1 | grep -E "PASS|FAIL"` — must show PASS
    Expected Result: Edge case tests exist and pass for empty NodeName and unscheduled pods
    Failure Indicators: Missing edge case tests, nil pointer dereference in test output
    Evidence: .sisyphus/evidence/task-6-indexer-edges.txt
  ```

  **Commit**: NO (groups with Wave 2)

- [x] 7. Write NodeReconciler TDD test skeleton + envtest setup

  **What to do**:
  - **Provision envtest binaries**: The repository has NO envtest setup. Before writing tests:
    1. Install `setup-envtest` tool: `go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest`
    2. Download envtest assets: `setup-envtest use --bin-dir ./testbin`
    3. Add a `testbin/` entry to `.gitignore` (binaries, do not vendor)
    4. Create a `Makefile` target or script `hack/setup-envtest.sh` that automates this
    5. Set `KUBEBUILDER_ASSETS` in test suite setup (e.g., in `TestMain` or a `BeforeSuite`)
    6. Alternative: use `envtest.Environment{BinaryAssetsDirectory: "./testbin"}` to point directly at downloaded binaries
  - **Provision CRD manifests for envtest**: The repo has NO local CRD YAML files. envtest needs CRD manifests loaded via `CRDDirectoryPaths` to accept MultiNetworkPolicy and NetworkAttachmentDefinition objects. Steps:
    1. Create directory `testdata/crds/`
    2. Fetch the MultiNetworkPolicy CRD YAML from upstream: `curl -sL https://raw.githubusercontent.com/k8snetworkplumbingwg/multi-networkpolicy/master/scheme.yml -o testdata/crds/multi-networkpolicy.yaml`
    3. Fetch the NetworkAttachmentDefinition CRD YAML from upstream: `curl -sL https://raw.githubusercontent.com/k8snetworkplumbingwg/network-attachment-definition-client/master/artifacts/networks-crd.yaml -o testdata/crds/network-attachment-definition.yaml`
    4. If upstream URLs change, alternatively generate CRD YAMLs manually using `apiextensionsv1.CustomResourceDefinition` in Go test code and write to temp dir
    5. In envtest.Environment, set `CRDDirectoryPaths: []string{"../../testdata/crds"}` (path relative to test file in `pkg/controller/`)
    6. Add `testdata/crds/*.yaml` to `.gitignore` OR commit them (prefer committing — they're small YAML, needed for CI)
    7. Verify envtest creates the CRDs: in test setup, check `cfg.Host` is non-empty and `client.List(ctx, &multinetv1beta1.MultiNetworkPolicyList{})` succeeds
  - Create file `pkg/controller/reconciler_test.go`
  - Use envtest (controller-runtime's test framework) to set up a test environment with API server
  - Register CRD schemes using the function from Task 2
  - Write test cases (initially failing — RED phase of TDD) for:
    1. `TestReconcile_NoPodsOnNode` — Reconcile returns success, no nftables calls
    2. `TestReconcile_PodWithNoPolicy` — Reconcile succeeds, no nftables rules applied
    3. `TestReconcile_PodWithMatchingPolicy` — Reconcile applies correct nftables rules (mock nftables)
    4. `TestReconcile_PodDeletedDuringReconcile` — Reconcile handles missing pod gracefully
    5. `TestReconcile_NamespaceSelector` — Policy with namespace selector filters correctly
  - Define a mock/fake `controllers.PolicyDeps` implementation for testing
  - Define the `NodeReconciler` struct signature that tests compile against (empty Reconcile method returning `ctrl.Result{}, nil`)
  - Tests should compile but FAIL (no implementation yet — that's Task 13)
  - **CI integration**: Update `.github/workflows/test.yml` to install envtest assets before running `go test`, OR use a `TestMain` function that calls `setup-envtest` programmatically

  **Must NOT do**:
  - DO NOT implement the actual Reconcile logic (that's Task 13)
  - DO NOT test nftables rule content (that's covered by existing netfilterrules_test.go)
  - DO NOT use real nftables operations in tests (mock the nftables interaction)

  **Recommended Agent Profile**:
  - **Category**: `deep`
    - Reason: TDD test design requires understanding the reconciler's expected behavior; envtest setup can be complex
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 2 (with T2, T3, T4, T5, T6)
  - **Blocks**: T13, T14
  - **Blocked By**: T1, T2, T3

  **References**:

  **Pattern References**:
  - `pkg/server/server.go:461-487` — `syncMultiPolicy()` — the current sync logic that the Reconciler replaces. Test cases should cover the same scenarios.
  - `pkg/server/server.go:540-600` — `applyPolicyRulesForPodAndFamily` — the per-pod policy application logic. Tests should verify Reconcile triggers this for matching pods.
  - `pkg/controllers/controller_suite_test.go` — Existing test setup pattern (Ginkgo/Gomega) — follow same style

  **API/Type References**:
  - `pkg/controller/deps.go` (from T3) — PolicyDeps interface — test mock must implement this
  - `pkg/controller/scheme.go` (from T2) — SetupScheme — needed for envtest setup

  **External References**:
  - controller-runtime envtest docs: `sigs.k8s.io/controller-runtime/pkg/envtest` — how to set up test environment, `CRDDirectoryPaths` for loading CRD manifests
  - CRD sources: `https://github.com/k8snetworkplumbingwg/multi-networkpolicy/blob/master/scheme.yml` (MultiNetworkPolicy CRD), `https://github.com/k8snetworkplumbingwg/network-attachment-definition-client/tree/master/artifacts` (NAD CRD)
  - openshift/multus-networkpolicy SHA 43b16450b7 — check their reconciler test patterns

  **WHY Each Reference Matters**:
  - `syncMultiPolicy()`: The behavior being replaced — test cases must cover equivalent scenarios
  - PolicyDeps interface: Tests need a mock implementation — must match T3's interface exactly
  - envtest: Required for controller-runtime reconciler testing without a real cluster

  **Acceptance Criteria**:

  **QA Scenarios (MANDATORY):**

  ```
  Scenario: envtest assets provisioned
    Tool: Bash
    Preconditions: T1 complete
    Steps:
      1. Run `which setup-envtest || go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest` — tool installed
      2. Run `ls testbin/ 2>/dev/null || setup-envtest use --bin-dir ./testbin` — assets present
      3. Run `grep "testbin" .gitignore` — testbin is gitignored
      4. Run `ls testdata/crds/` — must show at least 2 YAML files (multi-networkpolicy + network-attachment-definition)
      5. Run `grep "CustomResourceDefinition" testdata/crds/multi-networkpolicy.yaml` — CRD manifest valid
      6. Run `grep "CustomResourceDefinition" testdata/crds/network-attachment-definition.yaml` — CRD manifest valid
    Expected Result: envtest binaries present in testbin/, CRD manifests in testdata/crds/, tool installed, testbin gitignored
    Failure Indicators: Missing testbin/, missing CRD YAMLs, setup-envtest not found, CRD YAML invalid
    Evidence: .sisyphus/evidence/task-7-envtest-setup.txt

  Scenario: Test skeleton compiles
    Tool: Bash
    Preconditions: T1, T2, T3 complete, envtest assets provisioned
    Steps:
      1. Run `go test -v -list "Test.*" ./pkg/controller/... 2>&1` — should list 5+ test functions
      2. Run `go build ./pkg/controller/...` — must compile
    Expected Result: All test names listed, package compiles
    Failure Indicators: Compilation error, missing imports, interface mismatch
    Evidence: .sisyphus/evidence/task-7-test-skeleton.txt

  Scenario: Tests run with envtest (RED phase — no implementation yet)
    Tool: Bash
    Preconditions: Test skeleton created, Reconcile is stub, envtest assets available
    Steps:
      1. Run `KUBEBUILDER_ASSETS=$(setup-envtest use -p path) go test -v -run TestReconcile ./pkg/controller/... 2>&1 | tail -30`
      2. Verify tests either PASS (for no-op stub returning success) or FAIL (for tests expecting nftables calls)
    Expected Result: Tests compile and run against envtest API server; behavior tests fail because Reconcile is empty
    Failure Indicators: Tests panic, envtest binaries not found, won't compile
    Evidence: .sisyphus/evidence/task-7-red-phase.txt
  ```

  **Commit**: NO (groups with Wave 2)

- [x] 8. Refactor netfilterrules.go to use PolicyDeps + CommonRuleConfig AND extract applyPolicyRulesForPodAndFamily from Server

  **What to do**:
  **Part A — Signature refactoring in netfilterrules.go:**
  - In `pkg/server/netfilterrules.go`, change the 4 functions that accept `*Server` to accept the new interfaces:
    1. `applyCommonChainRules(s *Server)` → `applyCommonChainRules(cfg controllers.CommonRuleConfig)` — replace `s.Options.acceptICMPv6` with `cfg.AcceptICMPv6`, etc.
    2. `applyPolicyPeersRulesSelector(s *Server, ...)` → `applyPolicyPeersRulesSelector(deps controllers.PolicyDeps, ...)` — replace `s.podLister.Pods(...).List(...)` with `deps.ListPods(...)`, replace `s.namespaceMap.GetNamespaceInfo(...)` with `deps.GetNamespaceInfo(...)`, replace `s.podMap.GetPodInfo(...)` with `deps.GetPodInfo(...)`
    3. `applyPolicyPeersRules(s *Server, ...)` → `applyPolicyPeersRules(deps controllers.PolicyDeps, ...)` — pass `deps` through to `applyPolicyPeersRulesSelector`
    4. `applyPodRules(s *Server, ...)` → `applyPodRules(deps controllers.PolicyDeps, cfg controllers.CommonRuleConfig, ...)` — pass `deps` to `applyPolicyPeersRules`, `cfg` to `applyCommonChainRules`
  - **CRITICAL**: Remove mid-loop state mutations at lines 1248 and 1261:
    - Delete `s.namespaceMap.Update(s.nsChanges)` (L1248) — controller-runtime cache is always current
    - Delete `s.podMap.Update(s.podChanges)` (L1261) — controller-runtime cache is always current
  - Import `PolicyDeps` and `CommonRuleConfig` from `pkg/controllers/deps.go` — these are in the SAME package that `pkg/server` already imports (`pkg/controllers`), so NO new import is needed and NO import cycle is created

  **Part B — Extract applyPolicyRulesForPodAndFamily + Server→PolicyDeps bridge (merged from former T9):**
  - In `pkg/server/server.go`, refactor `applyPolicyRulesForPodAndFamily` (line 540) from a `*Server` method to a standalone exported function that accepts `PolicyDeps` + `CommonRuleConfig`:
    ```go
    func ApplyPolicyRulesForPodAndFamily(deps controllers.PolicyDeps, cfg controllers.CommonRuleConfig, policyMap controllers.PolicyMap, pod *v1.Pod, podInfo *controllers.PodInfo, nft *nftables.Conn) error
    ```
  - This function accesses `s.policyMap` (L555) — pass it as explicit parameter
  - It calls `nftState.applyCommonChainRules(s)` → change to `applyCommonChainRules(cfg)` (from Part A above)
  - It calls `nftState.applyPodRules(s, ...)` → change to `applyPodRules(deps, cfg, ...)` (from Part A above)
  - Also refactor `applyPolicyForPod` (line 490) and `applyPolicyRulesForPod` (line 518) similarly — they access `s.podMap`, `s.hostPrefix`
  - Keep the Server methods as thin wrappers calling the new standalone functions (Server implements `controllers.PolicyDeps` temporarily until Task 17 removes it)
  - Make Server implement the `controllers.PolicyDeps` interface by adding methods: `ListPods`, `GetNamespaceInfo`, `GetPodInfo` that delegate to existing `s.podLister`, `s.namespaceMap`, `s.podMap`
  - **IMPORTANT**: The standalone `ApplyPolicyRulesForPodAndFamily` function stays in `pkg/server/` — it's called by `pkg/controller/reconciler.go` (T13) which imports `pkg/server`. This is a one-way dependency: `pkg/controller` → `pkg/server` → `pkg/controllers`. No cycle.
  - **WHY MERGED**: T8's signature changes break the callers in server.go (lines ~604, ~616). Without updating those callers in the same task, `go build ./pkg/server/...` would fail. Merging ensures the task is self-contained and compilable.

  **Must NOT do**:
  - DO NOT change nftables rule generation logic (chain creation, set management, rule expressions)
  - DO NOT rename internal functions (only export the top-level `ApplyPolicyRulesForPodAndFamily`)
  - DO NOT reorder function parameters beyond replacing `*Server` with interface
  - DO NOT split or reorganize netfilterrules.go
  - DO NOT change any other logic in netfilterrules.go beyond the `*Server` replacement
  - DO NOT remove Server struct yet (that's Task 17)
  - DO NOT change function behavior — only change how data is passed

  **Recommended Agent Profile**:
  - **Category**: `deep`
    - Reason: 4 function signatures + caller updates + Server→PolicyDeps bridge + removing mid-loop mutations in a 1900-line file. Merged from two tasks for compilation safety.
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: NO (depends on T3, T4; blocks T10, T13)
  - **Parallel Group**: Wave 3 (with T10)
  - **Blocks**: T10, T13
  - **Blocked By**: T3, T4

  **References**:

  **Pattern References**:
  - `pkg/server/netfilterrules.go:823-868` — `applyCommonChainRules(s *Server)` — accesses `s.Options.acceptICMPv6` (L825), `s.Options.acceptICMP` (L840), `s.Options.allowSrcPrefix` (L849), `s.Options.allowDstPrefix` (L855). Replace `s.Options.*` with `cfg.*`.
  - `pkg/server/netfilterrules.go:1216-1295` — `applyPolicyPeersRulesSelector(s *Server, ...)` — accesses `s.podLister` (L1234), `s.namespaceMap` (L1248, L1253), `s.nsChanges` (L1248), `s.podMap` (L1261, L1262), `s.podChanges` (L1261). Replace with `deps.*` methods. DELETE L1248 and L1261 (mid-loop mutations).
  - `pkg/server/netfilterrules.go:1423` — `applyPolicyPeersRules(s *Server, ...)` — passes `s` through to `applyPolicyPeersRulesSelector`. Replace parameter.
  - `pkg/server/netfilterrules.go:1719` — `applyPodRules(s *Server, ...)` — passes `s` to `applyPolicyPeersRules` (L1753, L1768). Replace parameter.
  - `pkg/server/server.go:540-600` — `applyPolicyRulesForPodAndFamily` — accesses `s.policyMap` (L555), calls `nftState.applyCommonChainRules(s)`, `nftState.applyPodRules(s, ...)`. THIS is the caller that must be updated in the same task.
  - `pkg/server/server.go:490-516` — `applyPolicyForPod` — accesses `s.podMap.GetPodInfo(p)` (L491), `s.hostPrefix` (L500)
  - `pkg/server/server.go:518-538` — `applyPolicyRulesForPod` — passes `s` to `applyPolicyRulesForPodAndFamily`
  - `pkg/server/server.go:461-487` — `syncMultiPolicy` — calls `s.applyPolicyForPod(p)` — this call chain must still work via thin wrappers

  **API/Type References**:
  - `pkg/controllers/deps.go` (from T3) — `PolicyDeps` interface and `CommonRuleConfig` struct definitions
  - `pkg/controllers/pod.go:505-506` — `PodMap` type and `GetPodInfo` method

  **WHY Each Reference Matters**:
  - Each netfilterrules.go line reference shows exactly which `s.*` access to replace with which interface method — executor should do a 1:1 mapping
  - L1248, L1261: These are the mid-loop mutations that MUST be deleted — they're the key semantic change in the migration
  - `server.go:540-600`: The caller that breaks if only signatures change — MUST be updated in the same task to keep `go build` passing
  - `server.go:461-487`: Top-level sync function — must still work after refactoring (thin wrapper on Server)
  - **No import cycle risk**: `PolicyDeps` and `CommonRuleConfig` live in `pkg/controllers/` which `pkg/server` already imports

  **Acceptance Criteria**:

  **QA Scenarios (MANDATORY):**

  ```
  Scenario: No *Server parameter remains in netfilterrules.go
    Tool: Bash
    Preconditions: T3, T4 complete (PolicyDeps + CommonRuleConfig exist)
    Steps:
      1. Run `grep -n "s \*Server" pkg/server/netfilterrules.go` — must return empty
      2. Run `grep -n "PolicyDeps\|CommonRuleConfig" pkg/server/netfilterrules.go | head -10` — should show new parameter types
    Expected Result: Zero *Server parameters in netfilterrules.go, replaced with interface/struct
    Failure Indicators: Remaining *Server reference, import cycle
    Evidence: .sisyphus/evidence/task-8-server-removal.txt

  Scenario: Mid-loop mutations removed
    Tool: Bash
    Preconditions: Refactoring complete
    Steps:
      1. Run `grep -n "\.Update(s\." pkg/server/netfilterrules.go` — must return empty
      2. Run `grep -n "nsChanges\|podChanges" pkg/server/netfilterrules.go` — must return empty
    Expected Result: No ChangeTracker update calls remain in netfilterrules.go
    Failure Indicators: Any remaining mid-loop mutation
    Evidence: .sisyphus/evidence/task-8-mutations-removed.txt

  Scenario: Server implements controllers.PolicyDeps and everything compiles
    Tool: Bash
    Preconditions: T3, T4 complete, Part A and Part B both done
    Steps:
      1. Run `go build ./cmd/multi-networkpolicy-nftables/` — must compile
      2. Run `grep "func (s \*Server) ListPods" pkg/server/server.go` — Server implements PolicyDeps
      3. Run `grep "func (s \*Server) GetNamespaceInfo" pkg/server/server.go` — Server implements PolicyDeps
      4. Run `grep "func (s \*Server) GetPodInfo" pkg/server/server.go` — Server implements PolicyDeps
      5. Run `grep "var _ controllers.PolicyDeps" pkg/server/server.go` — compile-time check
    Expected Result: Server implements controllers.PolicyDeps interface, binary compiles, no import cycle
    Failure Indicators: Missing interface method, import cycle, compilation error, type mismatch
    Evidence: .sisyphus/evidence/task-8-server-policydeps.txt

  Scenario: Existing behavior preserved
    Tool: Bash
    Preconditions: All refactoring complete
    Steps:
      1. Run `sudo go test -v -count=1 ./pkg/server/... 2>&1 | tail -30` — all tests pass
    Expected Result: Existing tests pass — refactoring is behavior-preserving
    Failure Indicators: Any test failure
    Evidence: .sisyphus/evidence/task-8-behavior-preserved.txt
  ```

  **Commit**: NO (groups with Wave 3)

- [x] 9. ~~Extract applyPolicyRulesForPodAndFamily from Server~~ **(MERGED INTO T8)**

  > This task was merged into Task 8 to resolve a build gate conflict: T8's signature changes in netfilterrules.go broke the callers in server.go, making `go build` fail before T9 could update them. All T9 work (Server→PolicyDeps bridge, standalone function export, caller updates) is now in T8 Part B.
  >
  > **All references to "T9" in other tasks still apply — they now mean "T8 Part B".**

- [x] 10. Update existing unit tests for refactored interfaces

  **What to do**:
  - Update test files in `pkg/server/` that call the refactored functions (from T8)
  - Tests calling `applyCommonChainRules`, `applyPolicyPeersRules`, `applyPolicyPeersRulesSelector`, `applyPodRules`, `applyPolicyRulesForPodAndFamily` need to pass `PolicyDeps`/`CommonRuleConfig` instead of `*Server`
  - Create a test helper `testPolicyDeps` struct implementing `PolicyDeps` for use in existing tests
  - Create a test helper `testCommonRuleConfig()` function returning a default `CommonRuleConfig` for tests
  - Ensure ALL existing test assertions still hold — behavior must not change

  **Must NOT do**:
  - DO NOT add new test cases — only update existing tests to match new function signatures
  - DO NOT change test expectations or assertions
  - DO NOT refactor test structure — minimal changes only

  **Recommended Agent Profile**:
  - **Category**: `unspecified-high`
    - Reason: Must carefully update test call sites without changing behavior
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 3 (with T8)
  - **Blocks**: T13
  - **Blocked By**: T8

  **References**:

  **Pattern References**:
  - `pkg/server/netfilterrules_test.go` — Main test file for nftables rule generation. Search for calls to the 4 refactored functions and update parameters.
  - `pkg/server/server_test.go` (if exists) — Tests for server.go functions

  **Test References**:
  - `pkg/controllers/controller_suite_test.go` — Ginkgo suite setup pattern

  **API/Type References**:
  - `pkg/controllers/deps.go` — PolicyDeps interface, CommonRuleConfig struct — test mocks must implement/use these (note: same package as existing controllers types)

  **WHY Each Reference Matters**:
  - `netfilterrules_test.go`: Primary test file affected — every test calling refactored functions needs parameter updates
  - PolicyDeps: Test mocks need to satisfy this interface

  **Acceptance Criteria**:

  **QA Scenarios (MANDATORY):**

  ```
  Scenario: All existing nftables tests pass with new interfaces
    Tool: Bash
    Preconditions: T8 complete
    Steps:
      1. Run `sudo go test -v -count=1 ./pkg/server/... 2>&1 | tail -50` — all tests pass
      2. Run `go build ./cmd/multi-networkpolicy-nftables/` — binary compiles
    Expected Result: Zero test failures — behavior preserved through interface migration
    Failure Indicators: Any test failure, compilation error
    Evidence: .sisyphus/evidence/task-10-tests-pass.txt

  Scenario: No *Server references remain in test helper functions
    Tool: Bash
    Preconditions: Tests updated
    Steps:
      1. Run `grep -n "testPolicyDeps\|testCommonRuleConfig" pkg/server/netfilterrules_test.go | head -10` — helper usage visible
    Expected Result: Test helpers using new interface types
    Failure Indicators: Tests still constructing full *Server for nftables tests
    Evidence: .sisyphus/evidence/task-10-test-helpers.txt
  ```

  **Commit**: NO (groups with Wave 3)

- [ ] 11. Implement event map functions (mapToNode)

  **What to do**:
  - Create file `pkg/controller/mappers.go`
  - Implement map functions that translate resource events into Node reconcile requests:
    ```go
    func mapPodToNode(ctx context.Context, obj client.Object) []reconcile.Request
    func mapPolicyToNode(ctx context.Context, obj client.Object) []reconcile.Request
    func mapNamespaceToNode(ctx context.Context, obj client.Object) []reconcile.Request
    func mapNetDefToNode(ctx context.Context, obj client.Object) []reconcile.Request
    ```
  - `mapPodToNode`: Extract `pod.Spec.NodeName` → enqueue that Node. If NodeName is empty (unscheduled), return empty.
  - `mapPolicyToNode`, `mapNamespaceToNode`, `mapNetDefToNode`: These affect ALL nodes — return the single local Node reconcile request (the reconciler is node-scoped, running as DaemonSet)
  - The reconciler's node name will be stored in the `NodeReconciler` struct and accessed via a package-level function or closure. For now, use a `NodeName string` parameter captured in a closure when building the mapper.
  - Write unit tests in `pkg/controller/mappers_test.go`

  **Must NOT do**:
  - DO NOT query the API server from map functions — they should be pure data extraction
  - DO NOT enqueue multiple nodes — this is a DaemonSet, each instance only reconciles its own node

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: Small functions with clear input/output, well-defined controller-runtime pattern
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 4 (with T12, T13, T14)
  - **Blocks**: T14
  - **Blocked By**: T1

  **References**:

  **Pattern References**:
  - `pkg/server/server.go:479` — `multiutils.CheckNodeNameIdentical(s.Hostname, p.Spec.NodeName)` — current node-local filtering that `mapPodToNode` replaces

  **External References**:
  - openshift/multus-networkpolicy SHA 43b16450b7 — Uses `handler.EnqueueRequestsFromMapFunc` with custom map functions for Pod→Policy reconcile mapping
  - controller-runtime `handler.EnqueueRequestsFromMapFunc` API — signature: `func(context.Context, client.Object) []reconcile.Request`

  **WHY Each Reference Matters**:
  - `server.go:479`: Shows current node-filtering logic — `mapPodToNode` extracts `pod.Spec.NodeName` and creates reconcile.Request for that node
  - openshift reference: Shows how to structure map functions for the same domain

  **Acceptance Criteria**:

  **QA Scenarios (MANDATORY):**

  ```
  Scenario: Map functions compile and produce correct requests
    Tool: Bash
    Preconditions: T1 complete
    Steps:
      1. Run `go test -v -run TestMapPodToNode ./pkg/controller/...` — test passes
      2. Run `go test -v -run TestMapPolicyToNode ./pkg/controller/...` — test passes
      3. Run `go build ./pkg/controller/...` — package compiles
    Expected Result: mapPodToNode returns reconcile.Request with pod's NodeName; mapPolicyToNode returns request with local NodeName
    Failure Indicators: Wrong node name, empty request for scheduled pod, panic on nil
    Evidence: .sisyphus/evidence/task-11-mappers.txt

  Scenario: Unscheduled pod returns empty
    Tool: Bash
    Preconditions: Test exists
    Steps:
      1. Run `go test -v -run TestMapPodToNode_Unscheduled ./pkg/controller/... 2>&1 | grep -E "PASS|FAIL"` — must PASS (or equivalent test name)
      2. Run `grep -c "NodeName.*\"\"\|unscheduled\|empty.*NodeName" pkg/controller/mappers_test.go` — should be >= 1 (edge case test exists)
    Expected Result: Test verifies mapPodToNode returns empty slice for pods with no NodeName
    Failure Indicators: Non-empty result for unscheduled pod, missing test case
    Evidence: .sisyphus/evidence/task-11-unscheduled.txt
  ```

  **Commit**: NO (groups with Wave 4)

- [ ] 12. Implement predicates for watched resources

  **What to do**:
  - Create file `pkg/controller/predicates.go`
  - Implement custom predicates to filter irrelevant events:
    ```go
    func PodPredicate() predicate.Predicate  // Filter: only pods that are Running or being deleted
    func PolicyPredicate() predicate.Predicate  // Filter: generation changed (spec change, not status)
    func NodePredicate(nodeName string) predicate.Predicate  // Filter: only events for this node
    ```
  - `PodPredicate`: Accept Create/Update/Delete. On Update, skip if only status.phase unchanged and no label changes (avoid requeuing for irrelevant status updates like container restarts)
  - `PolicyPredicate`: Accept Create/Delete always. On Update, only accept if `ObjectMeta.Generation` changed (spec change) or annotations changed
  - `NodePredicate`: Accept only events for the node matching `nodeName` (the `.For(&Node{})` primary resource predicate)
  - Write unit tests in `pkg/controller/predicates_test.go`

  **Must NOT do**:
  - DO NOT over-filter — when in doubt, let events through (controller-runtime deduplicates anyway)
  - DO NOT add Namespace or NAD predicates — let all namespace/NAD events through (they're infrequent)

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: Small well-defined predicate functions following controller-runtime patterns
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES
  - **Parallel Group**: Wave 4 (with T11, T13, T14)
  - **Blocks**: T14
  - **Blocked By**: T1

  **References**:

  **External References**:
  - openshift/multus-networkpolicy SHA 43b16450b7 — Uses custom predicates for annotation/generation filtering
  - controller-runtime `predicate` package: `predicate.Funcs`, `predicate.GenerationChangedPredicate`

  **WHY Each Reference Matters**:
  - openshift reference: Shows which events to filter for the same domain — especially the generation-based policy filtering
  - controller-runtime predicates: `predicate.GenerationChangedPredicate` may be usable directly for PolicyPredicate

  **Acceptance Criteria**:

  **QA Scenarios (MANDATORY):**

  ```
  Scenario: Predicates filter correctly
    Tool: Bash
    Preconditions: T1 complete
    Steps:
      1. Run `go test -v -run TestPodPredicate ./pkg/controller/...` — passes
      2. Run `go test -v -run TestPolicyPredicate ./pkg/controller/...` — passes
      3. Run `go test -v -run TestNodePredicate ./pkg/controller/...` — passes
    Expected Result: All predicate tests pass with correct filter behavior
    Failure Indicators: Events that should be filtered are accepted, or vice versa
    Evidence: .sisyphus/evidence/task-12-predicates.txt

  Scenario: Policy predicate accepts generation changes, rejects status-only
    Tool: Bash
    Preconditions: Test exists
    Steps:
      1. Run `go test -v -run TestPolicyPredicate ./pkg/controller/... 2>&1` — full test output
      2. Run `grep -c "Generation\|generation\|Create\|Delete" pkg/controller/predicates_test.go` — should be >= 3 (tests cover generation, create, delete)
      3. Run `go test -v -run TestPolicyPredicate ./pkg/controller/... 2>&1 | grep -E "PASS|FAIL"` — must PASS
    Expected Result: Tests cover generation-changed (accept), status-only update (reject), create (accept), delete (accept)
    Failure Indicators: Status-only update accepted, or generation change rejected
    Evidence: .sisyphus/evidence/task-12-generation-filter.txt
  ```

  **Commit**: NO (groups with Wave 4)

- [ ] 13. Implement NodeReconciler.Reconcile() core logic

  **What to do**:
  - Create file `pkg/controller/reconciler.go`
  - Define `NodeReconciler` struct:
    ```go
    type NodeReconciler struct {
        client         client.Client
        nodeName       string
        hostPrefix     string
        networkPlugins []string
        criClient      pb.RuntimeServiceClient
        commonCfg      controllers.CommonRuleConfig
    }
    ```
  - Implement `func (r *NodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error)`:
    1. Verify `req.Name == r.nodeName` (safety check — predicates should ensure this)
    2. List pods on this node: `client.List(ctx, &v1.PodList{}, client.MatchingFields{PodHostnameIndex: r.nodeName})`
    3. For each pod where `IsMultiNetworkpolicyTarget(pod)` is true:
       a. Build PodInfo using `controllers.NewPodInfoFromPod(pod, r.criClient, r.nodeName, r.networkPlugins, r)` — pass `r` as `NetDefResolver` (see below)
       b. Resolve netns path (prepend `hostPrefix` if set)
       c. Open netns, create nftables connection
       d. Call `server.ApplyPolicyRulesForPodAndFamily(r, r.commonCfg, policyMap, pod, podInfo, nft)` (refactored in T8/T9, exported in pkg/server)
    4. Build policyMap by listing all MultiNetworkPolicies from cache
    5. NodeReconciler implements `controllers.PolicyDeps` interface:
       - `ListPods(selector)` → `r.client.List(ctx, &PodList{}, client.MatchingLabelsSelector{Selector: selector})` — use `client.MatchingLabelsSelector` wrapper (NOT `client.MatchingLabels` which expects `map[string]string`)
       - `GetNamespaceInfo(ns)` → `r.client.Get(ctx, nsKey, &ns)` → convert to `controllers.NamespaceInfo`
       - `GetPodInfo(pod)` → `controllers.NewPodInfoFromPod(pod, r.criClient, r.nodeName, r.networkPlugins, r)`
    6. NodeReconciler implements `controllers.NetDefResolver` interface:
       - `GetPluginType(namespacedName)` → `r.client.Get(ctx, namespacedName, &netdefv1.NetworkAttachmentDefinition{})` → parse NAD annotation/spec to extract plugin type (replicates `NetDefChangeTracker.GetPluginType` logic using cache read)
    7. Handle errors: log and continue for individual pod failures (don't fail entire reconciliation)
    8. Return `ctrl.Result{}` on success, `ctrl.Result{}, err` on fatal errors

  **Must NOT do**:
  - DO NOT implement nftables rule logic — call the existing refactored functions from `pkg/server`
  - DO NOT add leader election logic
  - DO NOT add custom metrics or health probes
  - DO NOT pass nil for NetDefResolver — implement it properly using cache reads

  **Recommended Agent Profile**:
  - **Category**: `deep`
    - Reason: Core reconciler logic with multiple dependencies; TDD GREEN phase — must satisfy tests from T7
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES (but depends on T6-T10)
  - **Parallel Group**: Wave 4 (with T11, T12, T14)
  - **Blocks**: T15
  - **Blocked By**: T6, T7, T8, T10

  **References**:

  **Pattern References**:
  - `pkg/server/server.go:461-487` — `syncMultiPolicy()` — the logic being replaced. The Reconciler's pod loop follows the same structure: list pods → filter by node → apply policies.
  - `pkg/server/server.go:490-538` — `applyPolicyForPod` + `applyPolicyRulesForPod` — the per-pod logic to call from Reconciler: get PodInfo, open netns, create nft connection, apply rules.
  - `pkg/server/server.go:540-600` — `applyPolicyRulesForPodAndFamily` (refactored in T9) — the function Reconciler calls for each pod
  - `pkg/controllers/pod.go:325-335` — `IsMultiNetworkpolicyTarget()` — pod filter predicate (running, not host-network)
  - `pkg/controller/indexes.go` (from T6) — `PodHostnameIndex` — use in `client.MatchingFields` for node-local pod query
  - `pkg/controllers/deps.go` (from T3) — PolicyDeps and NetDefResolver interfaces — NodeReconciler must implement both

  **API/Type References**:
  - `pkg/controllers/pod.go` — `PodInfo`, `InterfaceInfo` struct definitions
  - `pkg/controllers/networkpolicy.go` — `PolicyMap`, `PolicyInfo` — for building policyMap from cache
  - `pkg/controllers/namespace.go` — `NamespaceInfo` — for GetNamespaceInfo implementation
  - `pkg/controller/reconciler_test.go` (from T7) — Tests that must PASS after implementation (GREEN phase)

  **External References**:
  - openshift/multus-networkpolicy SHA 43b16450b7 — Reference their Reconcile() implementation for structural patterns
  - controller-runtime `client.List` with `MatchingFields` — efficient node-local pod query

  **WHY Each Reference Matters**:
  - `syncMultiPolicy()`: The exact logic being replaced — Reconciler must achieve the same result
  - `applyPolicyForPod` chain: The per-pod call sequence that Reconciler reuses
  - `IsMultiNetworkpolicyTarget`: Existing filter function to reuse directly
  - T7 tests: GREEN phase — implementation must pass the test cases defined there

  **Acceptance Criteria**:

  **QA Scenarios (MANDATORY):**

  ```
  Scenario: Reconciler tests pass (GREEN phase)
    Tool: Bash
    Preconditions: T3, T4, T6, T7, T8, T10 complete
    Steps:
      1. Run `KUBEBUILDER_ASSETS=$(setup-envtest use -p path) go test -v -run TestReconcile ./pkg/controller/... 2>&1` — all tests pass
      2. Run `go build ./pkg/controller/...` — package compiles
    Expected Result: All 5+ reconciler tests from T7 now pass
    Failure Indicators: Any test failure (means implementation doesn't match expected behavior)
    Evidence: .sisyphus/evidence/task-13-green-phase.txt

  Scenario: NodeReconciler satisfies PolicyDeps interface
    Tool: Bash
    Preconditions: Implementation complete
    Steps:
      1. Run `grep "var _ controllers.PolicyDeps = &NodeReconciler{}" pkg/controller/reconciler.go` — compile-time PolicyDeps interface check
      2. Run `grep "var _ controllers.NetDefResolver = &NodeReconciler{}" pkg/controller/reconciler.go` — compile-time NetDefResolver interface check
      3. Run `go vet ./pkg/controller/...` — no interface satisfaction errors
    Expected Result: NodeReconciler implements both PolicyDeps and NetDefResolver at compile time
    Failure Indicators: Missing method, wrong signature
    Evidence: .sisyphus/evidence/task-13-interface-check.txt
  ```

  **Commit**: NO (groups with Wave 4)

- [ ] 14. Implement NodeReconciler.SetupWithManager()

  **What to do**:
  - In `pkg/controller/reconciler.go` (same file as T13), implement:
    ```go
    func (r *NodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
        return ctrl.NewControllerManagedBy(mgr).
            For(&v1.Node{}, builder.WithPredicates(NodePredicate(r.nodeName))).
            Watches(&v1.Pod{}, handler.EnqueueRequestsFromMapFunc(mapPodToNode(r.nodeName)),
                builder.WithPredicates(PodPredicate())).
            Watches(&multiv1beta1.MultiNetworkPolicy{}, handler.EnqueueRequestsFromMapFunc(mapPolicyToNode(r.nodeName)),
                builder.WithPredicates(PolicyPredicate())).
            Watches(&netdefv1.NetworkAttachmentDefinition{}, handler.EnqueueRequestsFromMapFunc(mapNetDefToNode(r.nodeName))).
            Watches(&v1.Namespace{}, handler.EnqueueRequestsFromMapFunc(mapNamespaceToNode(r.nodeName))).
            Complete(r)
    }
    ```
  - Use map functions from T11 and predicates from T12
  - Use CRD types registered in scheme from T2
  - Write unit test verifying `SetupWithManager` doesn't panic/error with a fake manager

  **Must NOT do**:
  - DO NOT add custom rate limiters — use controller-runtime defaults
  - DO NOT add multiple concurrent reconcilers — one is sufficient for DaemonSet pattern
  - DO NOT add custom event handlers beyond the map functions

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: Straightforward wiring of already-implemented components
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES (but depends on T2, T7, T11, T12, T13)
  - **Parallel Group**: Wave 4 (with T11, T12, T13)
  - **Blocks**: T15
  - **Blocked By**: T2, T7, T11, T12, T13

  **References**:

  **Pattern References**:
  - `pkg/server/server.go:122-169` — `Run()` — current informer wiring that SetupWithManager replaces (SharedInformerFactory, Config handlers, etc.)
  - `pkg/controller/mappers.go` (from T11) — map functions to use
  - `pkg/controller/predicates.go` (from T12) — predicate functions to use
  - `pkg/controller/scheme.go` (from T2) — CRD type registration

  **External References**:
  - openshift/multus-networkpolicy SHA 43b16450b7 — Their `SetupWithManager` implementation — reference for `ctrl.NewControllerManagedBy` usage with CRD types
  - controller-runtime `builder` package: `For()`, `Watches()`, `WithPredicates()`, `Complete()`

  **WHY Each Reference Matters**:
  - `server.go:122-169`: The informer wiring being replaced — SetupWithManager must watch the same 4 resources
  - openshift reference: Production-proven `SetupWithManager` pattern for same domain

  **Acceptance Criteria**:

  **QA Scenarios (MANDATORY):**

  ```
  Scenario: SetupWithManager compiles and wires all resources
    Tool: Bash
    Preconditions: T2, T11, T12, T13 complete
    Steps:
      1. Run `go build ./pkg/controller/...` — must compile
      2. Run `grep -c "Watches" pkg/controller/reconciler.go` — should be 4 (Pod, Policy, NAD, Namespace)
      3. Run `grep "For(&v1.Node{}" pkg/controller/reconciler.go` — primary resource is Node
    Expected Result: Controller watches Node (primary) + 4 secondary resources
    Failure Indicators: Missing watch, wrong primary resource, compilation error
    Evidence: .sisyphus/evidence/task-14-setup.txt

  Scenario: SetupWithManager test passes
    Tool: Bash
    Preconditions: Implementation complete
    Steps:
      1. Run `go test -v -run TestSetupWithManager ./pkg/controller/...` — test passes
    Expected Result: SetupWithManager succeeds with fake manager, no panics
    Failure Indicators: Panic, missing scheme registration, nil pointer
    Evidence: .sisyphus/evidence/task-14-test.txt
  ```

  **Commit**: YES
  - Message: `feat(reconciler): implement NodeReconciler with controller-runtime`
  - Files: `pkg/controller/reconciler.go`, `pkg/controller/reconciler_test.go`, `pkg/controller/mappers.go`, `pkg/controller/mappers_test.go`, `pkg/controller/predicates.go`, `pkg/controller/predicates_test.go`
  - Pre-commit: `go test -v ./pkg/controller/...`

- [ ] 15. Wire controller-runtime Manager in cmd/main.go

  **What to do**:
  - Rewrite `cmd/multi-networkpolicy-nftables/main.go` to use controller-runtime Manager:
    ```go
    func main() {
        // Parse flags (keep existing cobra/pflag setup from Options)
        opts := server.NewOptions()
        // ... cobra command setup ...

        // Inside Run:
        mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
            Scheme:                 scheme,  // from T2
            LeaderElection:         false,   // DaemonSet — no leader election
            HealthProbeBindAddress: ":8081", // or disable
        })

        // Setup indexes (from T6)
        controller.SetupIndexes(ctx, mgr)

        // Create NodeReconciler
        reconciler := &controller.NodeReconciler{...}
        reconciler.SetupWithManager(mgr)

        // Start manager
        mgr.Start(ctrl.SetupSignalHandler())
    }
    ```
  - Preserve existing CLI flags from `Options.AddFlags()` — wire them into NodeReconciler and CommonRuleConfig
  - **CRITICAL — Unexported Options fields**: The fields needed for NodeReconciler construction (`master`, `hostnameOverride`, `hostPrefix`, `containerRuntime`, `containerRuntimeEndpoint`, `networkPlugins`, `acceptICMP`, `acceptICMPv6`, `allowSrcPrefix`, `allowDstPrefix`) are unexported in `pkg/server/options.go:36-53`. Fix this by adding a `BuildReconcilerConfig() ReconcilerConfig` method to `Options` that returns an exported struct with all needed values, OR export the fields directly. The method approach is cleaner:
    ```go
    // In pkg/server/options.go:
    type ReconcilerConfig struct {
        Kubeconfig               string
        Master                   string
        Hostname                 string
        HostPrefix               string
        ContainerRuntime         controllers.RuntimeKind
        ContainerRuntimeEndpoint string
        NetworkPlugins           []string
        PodIptables              string
        SyncPeriod               int
        CommonRuleConfig         controllers.CommonRuleConfig
    }
    func (o *Options) BuildReconcilerConfig() (*ReconcilerConfig, error)
    ```
    This method resolves hostname (via `nodeutil.GetHostname`), parses prefix lists, and packages everything. Called from main.go.
  - Build kubeconfig from Options (reuse existing `Kubeconfig`/`master` flag logic from `server.go:194-208`)
  - Use `ctrl.GetConfigOrDie()` or build config from Options fields
  - Create CRI client using existing `GetCriRuntimeClient()` function
  - Pass `CommonRuleConfig` populated from Options (acceptICMP, acceptICMPv6, allowSrcPrefix, allowDstPrefix)
  - Determine hostname using existing `nodeutil.GetHostname()` logic from `options.go`
  - Remove the cobra `Run` function that calls `opts.Run()` — replace with Manager startup
  - **Update deploy.yml RBAC**: Add `nodes: [get, list, watch]` to the ClusterRole. This is REQUIRED because `For(&v1.Node{})` in SetupWithManager causes controller-runtime to list/watch Node objects. Without this permission, the manager will fail to start with a forbidden error. Add the following rule to `deploy.yml` ClusterRole:
    ```yaml
    - apiGroups: [""]
      resources: ["nodes"]
      verbs: ["get", "list", "watch"]
    ```

  **Must NOT do**:
  - DO NOT add leader election (DaemonSet, one per node)
  - DO NOT add custom health/readiness probes beyond controller-runtime defaults
  - DO NOT remove Options struct or AddFlags — keep CLI interface unchanged
  - DO NOT change command-line flag names or defaults
  - DO NOT access unexported Options fields directly from main.go — use `BuildReconcilerConfig()` method

  **Recommended Agent Profile**:
  - **Category**: `deep`
    - Reason: Critical integration point — must wire all components correctly and preserve CLI behavior
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: NO
  - **Parallel Group**: Wave 5 (sequential start)
  - **Blocks**: T16, T17
  - **Blocked By**: T13, T14

  **References**:

  **Pattern References**:
  - `cmd/multi-networkpolicy-nftables/main.go:58-91` — Current main() — cobra setup, signal handling, KlogWriter. Keep cobra/pflag, replace the `opts.Run()` call with Manager.
  - `pkg/server/options.go:31-54` — Options struct — fields to wire into NodeReconciler: `Kubeconfig`, `master`, `hostnameOverride`, `hostPrefix`, `containerRuntime`, `containerRuntimeEndpoint`, `networkPlugins`, `podIptables`, `syncPeriod`, `acceptICMP`, `acceptICMPv6`, `allowSrcPrefix`, `allowDstPrefix`
  - `pkg/server/options.go:81-139` — `Run()` and `NewOptions()` — current startup logic: parse flags, build kubeconfig, create Server, start informers. Replace with Manager.
  - `pkg/server/server.go:194-208` — kubeconfig building logic — reuse in main.go for `ctrl.NewManager` config
  - `pkg/server/server.go:220-290` — NewServer() — current Server construction (CRI client, event broadcaster, etc.) — extract relevant parts into NodeReconciler construction
  - `pkg/controller/indexes.go` (from T6) — `SetupIndexes()` — call before starting manager
  - `pkg/controller/scheme.go` (from T2) — `SetupScheme()` — register CRD types in manager's scheme
  - `pkg/controller/reconciler.go` (from T13) — NodeReconciler constructor/setup

  **External References**:
  - controller-runtime `ctrl.NewManager` options — `LeaderElection: false` is critical for DaemonSet
  - `deploy.yml:7-23` — Current ClusterRole RBAC rules — MUST add `nodes: [get, list, watch]` for Node watch

  **WHY Each Reference Matters**:
  - `main.go`: The file being rewritten — must preserve CLI interface
  - `options.go`: All CLI flags that feed into NodeReconciler configuration
  - `server.go:194-208`: kubeconfig building pattern to reuse
  - `server.go:220-290`: CRI client creation, event broadcaster setup — decide which to keep
  - `deploy.yml`: RBAC must include nodes permission or controller-runtime Manager will fail to start

  **Acceptance Criteria**:

  **QA Scenarios (MANDATORY):**

  ```
  Scenario: Binary compiles and starts with --help
    Tool: Bash
    Preconditions: T2, T6, T13, T14 complete
    Steps:
      1. Run `go build -o /tmp/multi-networkpolicy-nftables ./cmd/multi-networkpolicy-nftables/` — must succeed
      2. Run `/tmp/multi-networkpolicy-nftables --help 2>&1` — must show existing flags
      3. Run `grep -c "container-runtime\|kubeconfig\|host-prefix\|network-plugins\|accept-icmp\|sync-period" <(/tmp/multi-networkpolicy-nftables --help 2>&1)` — should be >= 6
    Expected Result: Binary compiles, help shows all expected CLI flags
    Failure Indicators: Compilation error, missing flags, changed flag names
    Evidence: .sisyphus/evidence/task-15-binary.txt

  Scenario: Manager configured with LeaderElection disabled
    Tool: Bash
    Preconditions: Binary compiles
    Steps:
      1. Run `grep "LeaderElection.*false\|LeaderElection:.*false" cmd/multi-networkpolicy-nftables/main.go` — must match
    Expected Result: LeaderElection explicitly set to false
    Failure Indicators: LeaderElection true or not set (defaults to false but should be explicit)
    Evidence: .sisyphus/evidence/task-15-no-leader.txt

  Scenario: deploy.yml RBAC includes nodes permission
    Tool: Bash
    Preconditions: deploy.yml updated
    Steps:
      1. Run `grep -A2 '"nodes"' deploy.yml` — must show nodes resource with get, list, watch verbs
      2. Run `grep "nodes" deploy.yml` — must be present
    Expected Result: ClusterRole includes nodes: [get, list, watch]
    Failure Indicators: Missing nodes permission (manager will fail to start in cluster)
    Evidence: .sisyphus/evidence/task-15-rbac.txt
  ```

  **Commit**: YES
  - Message: `feat(main): wire controller-runtime Manager with NodeReconciler`
  - Files: `cmd/multi-networkpolicy-nftables/main.go`, `deploy.yml`, `pkg/server/options.go`
  - Pre-commit: `go build ./cmd/multi-networkpolicy-nftables/`

- [ ] 16. Implement graceful shutdown (nftables cleanup)

  **What to do**:
  - Implement shutdown logic that cleans up nftables rules from all pod network namespaces when the daemon stops
  - Current shutdown in `server.go:159-168`: sets `shuttingDown`, closes syncRunnerStopCh, then calls `syncMultiPolicy()` with `policyMap = nil` (which removes all nftables rules from pods)
  - In the new architecture, implement a shutdown hook via `mgr.Add(manager.RunnableFunc(...))` or a finalizer:
    1. When manager context is cancelled (SIGTERM), iterate all local pods
    2. For each pod with nftables rules, enter netns and delete the filter table
    3. This reuses the existing logic: calling `applyPolicyRulesForPodAndFamily` with an empty policyMap removes all rules
  - Alternatively, implement as a method on `NodeReconciler`: `func (r *NodeReconciler) Cleanup(ctx context.Context) error`
  - Register the cleanup via `mgr.Add()` as a `LeaderElectionRunnable` that runs on shutdown

  **Must NOT do**:
  - DO NOT leave stale nftables rules in pod namespaces after shutdown
  - DO NOT change the nftables cleanup logic — reuse existing pattern of empty policyMap sync
  - DO NOT add graceful shutdown timeout beyond what controller-runtime provides

  **Recommended Agent Profile**:
  - **Category**: `unspecified-high`
    - Reason: Requires understanding controller-runtime lifecycle hooks and existing cleanup pattern
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: YES (after T15)
  - **Parallel Group**: Wave 5 (with T17, T18, T19)
  - **Blocks**: T17
  - **Blocked By**: T15

  **References**:

  **Pattern References**:
  - `pkg/server/server.go:155-168` — Current shutdown logic: `s.shuttingDown.Store(true)`, `close(s.syncRunnerStopCh)`, `s.policyMap = nil`, `s.syncMultiPolicy()` — reuse the "empty policyMap" pattern
  - `pkg/server/server.go:461-487` — `syncMultiPolicy()` with nil policyMap — this effectively removes all nftables rules from all pods

  **External References**:
  - controller-runtime `manager.Runnable` interface — `mgr.Add()` for registering shutdown hooks
  - controller-runtime `LeaderElectionRunnable` — `NeedLeaderElection() bool` returning false (DaemonSet)

  **WHY Each Reference Matters**:
  - `server.go:155-168`: The exact cleanup pattern to replicate — empty policyMap causes rule removal
  - controller-runtime lifecycle: How to hook cleanup into manager shutdown sequence

  **Acceptance Criteria**:

  **QA Scenarios (MANDATORY):**

  ```
  Scenario: Cleanup function exists and is registered
    Tool: Bash
    Preconditions: T15 complete
    Steps:
      1. Run `grep -n "Cleanup\|cleanup\|mgr.Add" pkg/controller/reconciler.go cmd/multi-networkpolicy-nftables/main.go` — cleanup registration visible
      2. Run `go build ./cmd/multi-networkpolicy-nftables/` — compiles
    Expected Result: Cleanup function defined and registered with manager
    Failure Indicators: No cleanup registration, compilation error
    Evidence: .sisyphus/evidence/task-16-cleanup.txt

  Scenario: Cleanup uses empty policyMap pattern
    Tool: Bash
    Preconditions: Cleanup implemented
    Steps:
      1. Run `grep -n "policyMap\|PolicyMap\|nil\|empty" pkg/controller/reconciler.go | grep -i "clean\|shutdown\|stop"` — cleanup references empty/nil policyMap
      2. Run `grep -n "applyPolicyRulesForPodAndFamily\|ApplyPolicyRulesForPodAndFamily" pkg/controller/reconciler.go` — cleanup calls the existing apply function
      3. Run `go vet ./...` — no issues
    Expected Result: Cleanup function calls applyPolicyRulesForPodAndFamily with empty/nil policyMap to remove rules
    Failure Indicators: Different cleanup mechanism, no call to applyPolicyRulesForPodAndFamily in cleanup path
    Evidence: .sisyphus/evidence/task-16-cleanup-pattern.txt
  ```

  **Commit**: NO (groups with T15 commit)

- [ ] 17. Remove old code (ChangeTrackers, Configs, Handlers, Server, Runner)

  **What to do**:
  - **pkg/controllers/pod.go**: Remove `PodChangeTracker` struct + all its methods, `PodConfig` struct + all its methods, `PodHandler` interface, `PodMap.Update()` method. Keep: `PodInfo`, `InterfaceInfo`, `IsMultiNetworkpolicyTarget()`, `GetCriRuntimeClient()`, `NewPodInfoFromPod()` (from T4), `GetPodNetNSPath()` (from T4), `RuntimeKind` type.
  - **pkg/controllers/networkpolicy.go**: Remove `PolicyChangeTracker`, `NetworkPolicyConfig`, `NetworkPolicyHandler`. Keep: `PolicyInfo`, `PolicyMap` type.
  - **pkg/controllers/namespace.go**: Remove `NamespaceChangeTracker`, `NamespaceConfig`, `NamespaceHandler`. Keep: `NamespaceInfo`, `NamespaceMap.GetNamespaceInfo()`.
  - **pkg/controllers/net-attach-def.go**: Remove `NetDefChangeTracker`, `NetDefConfig`, `NetDefHandler`. Keep: `NetDefInfo` type.
  - **pkg/server/server.go**: Remove `Server` struct, `NewServer()`, `Run()`, `SyncLoop()`, `syncMultiPolicy()`, `applyPolicyForPod()`, `applyPolicyRulesForPod()`, all `On*Add/Update/Delete/Synced` handler methods, `RunPodConfig()`, `birthCry()`, `setInitialized()`, `isInitialized()`. Keep: `internalPolicy` struct, `CompareInternalPolicy()`, `getEnabledPolicyTypes()`, `podNamespacedName()`, `policyNamespacedName()`, standalone refactored functions from T9.
  - **pkg/server/options.go**: Remove `Run()` and `Stop()` methods. Keep: `Options` struct, `NewOptions()`, `AddFlags()`.
  - Remove `k8s.io/kubernetes/pkg/proxy/runner` import from server.go
  - Remove all imports that become unused after deletions
  - Verify `go build` succeeds after each major deletion

  **Must NOT do**:
  - DO NOT remove types still used (PodInfo, PolicyInfo, NamespaceInfo, etc.)
  - DO NOT remove netfilterrules.go content
  - DO NOT remove encoder.go
  - DO NOT remove pkg/utils/
  - DO NOT remove test files yet — they may need updating alongside
  - DO NOT remove code that the refactored standalone functions depend on

  **Recommended Agent Profile**:
  - **Category**: `unspecified-high`
    - Reason: Large-scale deletion across multiple files; requires careful dependency tracing to avoid breaking compilation
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: NO (sequential after T15, T16)
  - **Parallel Group**: Wave 5 (sequential)
  - **Blocks**: T18
  - **Blocked By**: T15, T16

  **References**:

  **Pattern References**:
  - `pkg/controllers/pod.go:76-91` — `PodHandler` interface — REMOVE
  - `pkg/controllers/pod.go:93-179` — `PodConfig` struct + methods — REMOVE
  - `pkg/controllers/pod.go:180-503` — `PodChangeTracker` + methods + `PodMap.Update()` — REMOVE (keep PodMap type, GetPodInfo)
  - `pkg/controllers/networkpolicy.go` — search for `PolicyChangeTracker`, `NetworkPolicyConfig`, `NetworkPolicyHandler` — REMOVE
  - `pkg/controllers/namespace.go` — search for `NamespaceChangeTracker`, `NamespaceConfig`, `NamespaceHandler` — REMOVE
  - `pkg/controllers/net-attach-def.go` — search for `NetDefChangeTracker`, `NetDefConfig`, `NetDefHandler` — REMOVE
  - `pkg/server/server.go:65-98` — `Server` struct — REMOVE
  - `pkg/server/server.go:109-191` — Server methods (RunPodConfig, Run, SyncLoop, etc.) — REMOVE
  - `pkg/server/server.go:194-460` — NewServer, handler methods — REMOVE
  - `pkg/server/server.go:461-600` — syncMultiPolicy, applyPolicyForPod chain — REMOVE (standalone versions exist from T9)
  - `pkg/server/server.go:58` — `"k8s.io/kubernetes/pkg/proxy/runner"` import — REMOVE

  **WHY Each Reference Matters**:
  - Each reference is a specific deletion target. Executor must verify each removed item has no remaining callers. Use `grep` and `go build` after each major deletion to catch breakage early.

  **Acceptance Criteria**:

  **QA Scenarios (MANDATORY):**

  ```
  Scenario: All legacy types removed
    Tool: Bash
    Preconditions: T15, T16 complete
    Steps:
      1. Run `grep -n "ChangeTracker" pkg/controllers/*.go | grep -v _test.go | grep -v "//\|func.*Test"` — must return empty
      2. Run `grep -n "BoundedFrequencyRunner\|syncRunner" pkg/server/*.go | grep -v _test.go` — must return empty
      3. Run `grep -n "type Server struct" pkg/server/server.go` — must return empty
      4. Run `grep -n "PodConfig\|NetworkPolicyConfig\|NamespaceConfig\|NetDefConfig" pkg/controllers/*.go | grep -v _test.go` — must return empty
      5. Run `grep -n "PodHandler\|NetworkPolicyHandler\|NamespaceHandler\|NetDefHandler" pkg/controllers/*.go | grep -v _test.go` — must return empty
    Expected Result: Zero matches for all legacy types in non-test files
    Failure Indicators: Any remaining legacy type definition or usage
    Evidence: .sisyphus/evidence/task-17-legacy-removed.txt

  Scenario: Project still compiles and tests pass
    Tool: Bash
    Preconditions: Deletions complete
    Steps:
      1. Run `go build ./cmd/multi-networkpolicy-nftables/` — must succeed
      2. Run `sudo go test -v -count=1 ./... 2>&1 | tail -50` — check results (some old tests may be removed)
      3. Run `go vet ./...` — no issues
    Expected Result: Binary compiles, remaining tests pass
    Failure Indicators: Compilation error from missing type, unused import, broken test
    Evidence: .sisyphus/evidence/task-17-compiles.txt
  ```

  **Commit**: YES
  - Message: `refactor(cleanup): remove legacy informer infrastructure and Server struct`
  - Files: `pkg/controllers/pod.go`, `pkg/controllers/networkpolicy.go`, `pkg/controllers/namespace.go`, `pkg/controllers/net-attach-def.go`, `pkg/server/server.go`, `pkg/server/options.go`
  - Pre-commit: `go build ./cmd/multi-networkpolicy-nftables/`

- [ ] 18. Remove k8s.io/kubernetes dep + go mod tidy + vendor

  **What to do**:
  - Verify no Go files still import any `k8s.io/kubernetes/...` package: `grep -rn "k8s.io/kubernetes" pkg/ cmd/ --include="*.go"`
  - If any imports remain, remove them (the only usage was `k8s.io/kubernetes/pkg/proxy/runner` for BoundedFrequencyRunner and `k8s.io/kubernetes/pkg/apis/core` for EventTypeNormal — both removed in T17)
  - Note: `api "k8s.io/kubernetes/pkg/apis/core"` was used in `birthCry()` for `api.EventTypeNormal` — this was removed with Server in T17. If any remaining usage, replace `api.EventTypeNormal` with `v1.EventTypeNormal` (from `k8s.io/api/core/v1`)
  - Remove `k8s.io/kubernetes` from `go.mod` require block
  - Run `go mod tidy` to clean unused dependencies
  - Run `go mod vendor` to update vendor directory
  - Verify `go build` succeeds and all tests pass

  **Must NOT do**:
  - DO NOT remove other k8s.io/* dependencies (api, apimachinery, client-go, etc.)
  - DO NOT manually edit go.sum — let `go mod tidy` handle it

  **Recommended Agent Profile**:
  - **Category**: `quick`
    - Reason: Simple dependency removal + tidy
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: NO (after T17)
  - **Parallel Group**: Wave 5 (sequential after T17)
  - **Blocks**: T19
  - **Blocked By**: T17

  **References**:

  **Pattern References**:
  - `go.mod:28` — `k8s.io/kubernetes v1.35.1` — the line to remove
  - `pkg/server/server.go:58` — `"k8s.io/kubernetes/pkg/proxy/runner"` import (should already be removed by T17)
  - `pkg/server/server.go:58` — `api "k8s.io/kubernetes/pkg/apis/core"` import (should already be removed by T17)

  **WHY Each Reference Matters**:
  - `go.mod:28`: The dependency being removed — massive dep tree reduction
  - server.go imports: Must verify these are gone before `go mod tidy` will drop the dependency

  **Acceptance Criteria**:

  **QA Scenarios (MANDATORY):**

  ```
  Scenario: k8s.io/kubernetes completely removed
    Tool: Bash
    Preconditions: T17 complete
    Steps:
      1. Run `grep -rn "k8s.io/kubernetes" pkg/ cmd/ --include="*.go"` — must return empty
      2. Run `grep "k8s.io/kubernetes" go.mod` — must return empty
      3. Run `ls vendor/k8s.io/kubernetes/ 2>&1` — should say "No such file or directory"
      4. Run `go build ./cmd/multi-networkpolicy-nftables/` — must succeed
    Expected Result: Zero references to k8s.io/kubernetes in code, go.mod, or vendor
    Failure Indicators: Remaining import, go.mod reference, vendor directory
    Evidence: .sisyphus/evidence/task-18-k8s-removed.txt

  Scenario: go.mod is clean and build works
    Tool: Bash
    Preconditions: Dependency removed
    Steps:
      1. Run `go mod verify` — must succeed
      2. Run `sudo go test -v -count=1 ./... 2>&1 | tail -30` — tests pass
    Expected Result: Module verification passes, all tests pass
    Failure Indicators: Missing transitive dependency, test failure
    Evidence: .sisyphus/evidence/task-18-mod-clean.txt
  ```

  **Commit**: YES
  - Message: `build(deps): remove k8s.io/kubernetes monorepo dependency`
  - Files: `go.mod`, `go.sum`, `vendor/`
  - Pre-commit: `go build ./cmd/multi-networkpolicy-nftables/`

- [ ] 19. Migrate klog v2 → logr (mechanical translation)

  **What to do**:
  - Replace ALL `klog.Infof(...)`, `klog.Errorf(...)`, `klog.Warningf(...)`, `klog.V(N).Infof(...)` calls with logr equivalents:
    - `klog.Infof("msg %s", arg)` → `log.Info("msg", "key", arg)` (structured key-value pairs)
    - `klog.Errorf("msg %v", err)` → `log.Error(err, "msg")`
    - `klog.V(N).Infof("msg")` → `log.V(N).Info("msg")`
    - `klog.Warningf("msg")` → `log.Info("msg")` (logr has no Warning level — use Info or annotate with key)
  - Wait — per guardrails: **mechanical 1:1 translation only, DO NOT reword messages**
  - Actually, use the simpler approach: `klog.SetLogger(logr.Logger)` in main.go to bridge klog→logr, then all existing klog calls automatically go through logr. This avoids touching every file.
  - Approach:
    1. In main.go, after creating Manager: `klog.SetLogger(mgr.GetLogger())` — this bridges klog to logr
    2. Remove `KlogWriter` struct and `initLogs()` function — no longer needed
    3. Remove `klog.Flush` calls — logr doesn't need flushing
    4. This is the minimal change that achieves logr logging without touching every file
  - Later (optional, not in this plan): gradually replace klog calls with direct logr calls in each file

  **Must NOT do**:
  - DO NOT reword log messages
  - DO NOT change verbosity levels
  - DO NOT add structured key-value pairs to existing log calls (mechanical only)
  - DO NOT remove klog/v2 import — it's still used for the bridge

  **Recommended Agent Profile**:
  - **Category**: `unspecified-high`
    - Reason: Requires understanding klog→logr bridge setup and careful main.go changes
  - **Skills**: []

  **Parallelization**:
  - **Can Run In Parallel**: NO (after T18)
  - **Parallel Group**: Wave 5 (last task)
  - **Blocks**: F1-F4
  - **Blocked By**: T5, T18

  **References**:

  **Pattern References**:
  - `cmd/multi-networkpolicy-nftables/main.go:39-56` — `KlogWriter`, `initLogs()` — REMOVE (logr replaces this)
  - `cmd/multi-networkpolicy-nftables/main.go:59-60` — `initLogs()`, `defer klog.Flush()` — REMOVE

  **External References**:
  - klog v2 → logr bridge: `klog.SetLogger(logger)` — official bridge mechanism
  - controller-runtime `mgr.GetLogger()` — provides logr.Logger instance

  **WHY Each Reference Matters**:
  - `main.go:39-56`: Code to remove — KlogWriter is a klog v1 pattern replaced by logr bridge
  - `klog.SetLogger`: The key API call — one line that bridges all existing klog calls to logr

  **Acceptance Criteria**:

  **QA Scenarios (MANDATORY):**

  ```
  Scenario: logr bridge configured in main.go
    Tool: Bash
    Preconditions: T5, T18 complete
    Steps:
      1. Run `grep "klog.SetLogger\|SetLogger" cmd/multi-networkpolicy-nftables/main.go` — bridge call present
      2. Run `grep -c "KlogWriter\|initLogs" cmd/multi-networkpolicy-nftables/main.go` — should be 0 (removed)
      3. Run `go build ./cmd/multi-networkpolicy-nftables/` — must compile
    Expected Result: logr bridge configured, old KlogWriter removed, compiles
    Failure Indicators: Missing bridge call, KlogWriter still present, compilation error
    Evidence: .sisyphus/evidence/task-19-logr-bridge.txt

  Scenario: All tests pass with logr
    Tool: Bash
    Preconditions: Bridge configured
    Steps:
      1. Run `sudo go test -v -count=1 ./... 2>&1 | tail -30` — all tests pass
    Expected Result: Logging still works, no test failures
    Failure Indicators: Missing log output, panic on uninitialized logger
    Evidence: .sisyphus/evidence/task-19-tests.txt
  ```

  **Commit**: YES
  - Message: `refactor(logging): bridge klog to logr via controller-runtime`
  - Files: `cmd/multi-networkpolicy-nftables/main.go`
  - Pre-commit: `go build ./cmd/multi-networkpolicy-nftables/`

---

## Final Verification Wave (MANDATORY — after ALL implementation tasks)

> 4 review agents run in PARALLEL. ALL must APPROVE. Present consolidated results to user and get explicit "okay" before completing.
>
> **Do NOT auto-proceed after verification. Wait for user's explicit approval before marking work complete.**

- [ ] F1. **Plan Compliance Audit** — `oracle`
  Read the plan end-to-end. For each "Must Have": verify implementation exists (read file, run command). For each "Must NOT Have": search codebase for forbidden patterns — reject with file:line if found. Check evidence files exist in .sisyphus/evidence/. Compare deliverables against plan.
  Output: `Must Have [N/N] | Must NOT Have [N/N] | Tasks [N/N] | VERDICT: APPROVE/REJECT`

- [ ] F2. **Code Quality Review** — `unspecified-high`
  Run `go build ./cmd/multi-networkpolicy-nftables/` + `golangci-lint run` + `sudo go test -v -count=1 ./...`. Review all changed files for: `as any`/type assertions without checks, empty catches, commented-out code, unused imports. Check AI slop: excessive comments, over-abstraction, generic names.
  Output: `Build [PASS/FAIL] | Lint [PASS/FAIL] | Tests [N pass/N fail] | Files [N clean/N issues] | VERDICT`

- [ ] F3. **Real Manual QA** — `unspecified-high`
  Start from clean state. Execute EVERY QA scenario from EVERY task — follow exact steps, capture evidence. Test cross-task integration (reconciler + nftables + CRI working together). Test edge cases: empty state, pod deletion mid-reconcile, rapid policy changes. Save to `.sisyphus/evidence/final-qa/`.
  Output: `Scenarios [N/N pass] | Integration [N/N] | Edge Cases [N tested] | VERDICT`

- [ ] F4. **Scope Fidelity Check** — `deep`
  For each task: read "What to do", read actual diff (git log/diff). Verify 1:1 — everything in spec was built (no missing), nothing beyond spec was built (no creep). Check "Must NOT do" compliance. Detect cross-task contamination: Task N touching Task M's files. Flag unaccounted changes.
  Output: `Tasks [N/N compliant] | Contamination [CLEAN/N issues] | Unaccounted [CLEAN/N files] | VERDICT`

---

## Commit Strategy

- **T1**: `build(deps): add controller-runtime dependency` — go.mod, go.sum, vendor/
- **T2-T7**: `refactor(foundation): {task-specific}` — grouped commit after all Wave 2 tasks
- **T8-T10**: `refactor(nftables): decouple nftables engine from Server struct` — pkg/server/netfilterrules.go, pkg/server/server.go, tests
- **T11-T14**: `feat(reconciler): implement NodeReconciler with controller-runtime` — pkg/controller/
- **T15-T16**: `feat(main): wire controller-runtime Manager with graceful shutdown` — cmd/main.go
- **T17-T18**: `refactor(cleanup): remove legacy informer infrastructure` — pkg/controllers/, pkg/server/, go.mod
- **T19**: `refactor(logging): migrate klog to logr structured logging` — all .go files

---

## Success Criteria

### Verification Commands
```bash
go build ./cmd/multi-networkpolicy-nftables/                    # Expected: success
golangci-lint run                                                # Expected: zero violations
sudo go test -v -count=1 ./...                                   # Expected: all pass
cd e2e && ./run_all_tests.sh                                     # Expected: all 16+ tests pass
grep -rn "k8s.io/kubernetes" pkg/ cmd/ --include="*.go"          # Expected: empty
grep -n "ChangeTracker\|BoundedFrequencyRunner" pkg/ cmd/ --include="*.go" | grep -v _test.go  # Expected: empty
grep -n "func.*\*Server" pkg/server/netfilterrules.go            # Expected: empty
```

### Final Checklist
- [ ] All "Must Have" present
- [ ] All "Must NOT Have" absent
- [ ] All unit tests pass
- [ ] All e2e tests pass
- [ ] No k8s.io/kubernetes dependency remains
- [ ] No ChangeTracker/BoundedFrequencyRunner code remains
- [ ] netfilterrules.go has no *Server parameters
- [ ] controller-runtime Manager starts with LeaderElection: false
- [ ] Graceful shutdown cleans up nftables rules
