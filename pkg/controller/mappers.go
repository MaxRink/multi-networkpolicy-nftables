package controller

import (
	"context"

	multiv1beta1 "github.com/k8snetworkplumbingwg/multi-networkpolicy/pkg/apis/k8s.cni.cncf.io/v1beta1"
	netdefv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// mapPodToNode maps a Pod event to a reconcile.Request for the pod's node.
// Pods not yet scheduled (empty NodeName) produce no request.
func mapPodToNode(_ string) handler.MapFunc {
	return func(_ context.Context, obj client.Object) []reconcile.Request {
		pod, ok := obj.(*corev1.Pod)
		if !ok || pod.Spec.NodeName == "" {
			return nil
		}

		return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: pod.Spec.NodeName}}}
	}
}

// mapPolicyToNode maps a MultiNetworkPolicy event to a reconcile.Request for the local node.
func mapPolicyToNode(nodeName string) handler.MapFunc {
	return func(_ context.Context, obj client.Object) []reconcile.Request {
		_, ok := obj.(*multiv1beta1.MultiNetworkPolicy)
		if !ok {
			return nil
		}
		return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: nodeName}}}
	}
}

// mapNamespaceToNode maps a Namespace event to a reconcile.Request for the local node.
func mapNamespaceToNode(nodeName string) handler.MapFunc {
	return func(_ context.Context, obj client.Object) []reconcile.Request {
		_, ok := obj.(*corev1.Namespace)
		if !ok {
			return nil
		}
		return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: nodeName}}}
	}
}

// mapNetDefToNode maps a NetworkAttachmentDefinition event to a reconcile.Request for the local node.
func mapNetDefToNode(nodeName string) handler.MapFunc {
	return func(_ context.Context, obj client.Object) []reconcile.Request {
		_, ok := obj.(*netdefv1.NetworkAttachmentDefinition)
		if !ok {
			return nil
		}
		return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: nodeName}}}
	}
}
