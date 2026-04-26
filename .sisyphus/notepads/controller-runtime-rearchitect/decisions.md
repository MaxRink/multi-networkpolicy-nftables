# Decisions

## [2026-04-26] Session Start
- Import cycle solution: pkg/controllers/ (shared types), pkg/server/ imports pkg/controllers, pkg/controller/ imports both. All one-way.
- Reconciler design: Node-anchored full-sweep. `.For(&v1.Node{})` with NodePredicate. Other resources enqueue Node via mapToNode funcs.
- PolicyDeps interface: ListPods(selector), GetNamespaceInfo(namespace), GetPodInfo(pod) — derived from netfilterrules.go *Server access
- NetDefResolver interface: GetPluginType(NamespacedName) string — replaces NetDefChangeTracker.GetPluginType in newPodInfo
- Options.BuildReconcilerConfig() method exports unexported Options fields for main.go
- T8 merged with T9: must update both netfilterrules.go AND server.go callers in same task
- LeaderElection: false (DaemonSet)
- klog v1 → v2 first (T5), then bridge to logr at end (T19) via klog.SetLogger(mgr.GetLogger())
- envtest CRD loading: CRDDirectoryPaths pointing to testdata/crds/ with fetched upstream YAML files
