//go:build !linux

package controller

import (
	"context"
	"fmt"
	"runtime"

	klog "k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func cleanupAllPods(_ context.Context, _ *NodeReconciler, _ client.Client) error {
	return fmt.Errorf("nftables cleanup is unsupported on %s", runtime.GOOS)
}
