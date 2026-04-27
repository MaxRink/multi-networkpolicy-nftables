package controllers

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
)

type PolicyDeps interface {
	ListPods(selector labels.Selector) ([]*corev1.Pod, error)
	GetNamespaceInfo(namespace string) (*NamespaceInfo, error)
	GetPodInfo(pod *corev1.Pod) (*PodInfo, error)
}

type CommonRuleConfig struct {
	AcceptICMPv6   bool
	AcceptICMP     bool
	AllowSrcPrefix []string
	AllowDstPrefix []string
}

type NetDefResolver interface {
	GetPluginType(namespacedName types.NamespacedName) string
}
