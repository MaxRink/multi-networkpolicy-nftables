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
}

// NetDefResolver resolves CNI plugin metadata for network attachments.
type NetDefResolver interface {
	GetPluginType(ctx context.Context, namespacedName types.NamespacedName) (string, error)
}
