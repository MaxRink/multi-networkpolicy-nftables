//go:build !linux

package controller

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

func cleanupAllPods(_ context.Context, _ *NodeReconciler, _ client.Client) error { return nil }
