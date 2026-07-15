package controllers

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
)

// PolicyDeps provides cluster lookups needed while rendering policy rules.
type PolicyDeps interface {
	ListPods(ctx context.Context, selector labels.Selector) ([]*corev1.Pod, error)
	GetNamespaceInfo(ctx context.Context, namespace string) (*NamespaceInfo, error)
	GetPodInfo(ctx context.Context, pod *corev1.Pod) (*PodInfo, error)
}

// CommonRuleConfig contains rule options that are shared across policy renders.
type CommonRuleConfig struct {
	AcceptICMPv6   bool
	AcceptICMP     bool
	AllowSrcPrefix []string
	AllowDstPrefix []string

	// CTEnabled selects the stateful, connection-tracking (CT offload) tc
	// flower pipeline instead of the stateless first-match pipeline. It is
	// consumed only by the tc flower backend (the nft backend is always
	// stateful via its own conntrack rules); the nft engine ignores it. The
	// zero value keeps the tc backend stateless, so existing callers/tests are
	// unaffected. The tc applier flips it on by default (see pkg/controller).
	CTEnabled bool
}

// NetDefResolver resolves CNI plugin metadata for network attachments.
type NetDefResolver interface {
	GetPluginType(ctx context.Context, namespacedName types.NamespacedName) (string, error)
}
