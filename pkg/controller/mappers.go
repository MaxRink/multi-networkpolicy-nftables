package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// mapToNode maps cluster-wide dependency events to the local node. Pod events
// use it too: a pod on any node can be a peer of the local node's policies.
func mapToNode(nodeName string) handler.MapFunc {
	return func(context.Context, client.Object) []reconcile.Request {
		return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: nodeName}}}
	}
}
