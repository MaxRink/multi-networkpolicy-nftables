//go:build !linux

package controller

import (
	"context"

	klog "k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func cleanupAllPods(_ context.Context, _ *NodeReconciler, _ client.Client) error { return nil }

func debugLog(_, format string, args ...interface{}) {
	klog.Infof(format, args...)
}
