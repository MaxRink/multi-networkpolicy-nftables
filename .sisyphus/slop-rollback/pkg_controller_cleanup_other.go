//go:build !linux

package controller

import "context"

func cleanupAllPods(_ context.Context, _ *NodeReconciler) error { return nil }
