package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// mapPodToNode maps a Pod event to a reconcile.Request for the pod's node.
// Pods not yet scheduled (empty NodeName) produce no request.
func mapPodToNode() handler.MapFunc {
	return func(_ context.Context, obj client.Object) []reconcile.Request {
		pod, ok := obj.(*corev1.Pod)
		if !ok || pod.Spec.NodeName == "" {
			return nil
		}

		return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: pod.Spec.NodeName}}}
	}
}

// mapToNode maps cluster-wide dependency events to the local node.
func mapToNode(nodeName string) handler.MapFunc {
	return func(context.Context, client.Object) []reconcile.Request {
		return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: nodeName}}}
	}
}
