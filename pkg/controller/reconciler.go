package controller

import (
	"context"

	"github.com/telekom/multi-networkpolicy-nftables/pkg/controllers"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type NodeReconciler struct {
	NodeName   string
	Client     client.Client
	PolicyDeps controllers.PolicyDeps
}

func (r *NodeReconciler) Reconcile(_ context.Context, _ reconcile.Request) (reconcile.Result, error) {
	return reconcile.Result{}, nil
}
