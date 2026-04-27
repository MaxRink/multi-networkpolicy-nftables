package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const PodHostnameIndex = "spec.nodeName"

func podHostnameExtractor(obj client.Object) []string {
	pod := obj.(*corev1.Pod)
	if pod.Spec.NodeName == "" {
		return nil
	}
	return []string{pod.Spec.NodeName}
}

func SetupIndexes(ctx context.Context, mgr ctrl.Manager) error {
	return mgr.GetFieldIndexer().IndexField(ctx, &corev1.Pod{}, PodHostnameIndex, podHostnameExtractor)
}
