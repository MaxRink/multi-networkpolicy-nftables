package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PodHostnameIndex is the field index used to list pods scheduled onto a node.
const PodHostnameIndex = "spec.nodeName"

func podHostnameExtractor(obj client.Object) []string {
	pod := obj.(*corev1.Pod)
	if pod.Spec.NodeName == "" {
		return nil
	}
	return []string{pod.Spec.NodeName}
}

// SetupIndexes registers controller-runtime field indexes used by the reconciler.
func SetupIndexes(ctx context.Context, mgr ctrl.Manager) error {
	return mgr.GetFieldIndexer().IndexField(ctx, &corev1.Pod{}, PodHostnameIndex, podHostnameExtractor)
}
