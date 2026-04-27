//go:build linux

package controller

import (
	"context"

	"github.com/telekom/multi-networkpolicy-nftables/pkg/controllers"
	corev1 "k8s.io/api/core/v1"
	klog "k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func cleanupAllPods(ctx context.Context, r *NodeReconciler) error {
	var podList corev1.PodList
	if err := r.Client.List(ctx, &podList, client.MatchingFields{PodHostnameIndex: r.NodeName}); err != nil {
		return err
	}
	deps := r.policyDeps()
	for i := range podList.Items {
		pod := &podList.Items[i]
		if !controllers.IsMultiNetworkpolicyTarget(pod) {
			continue
		}
		podInfo, err := deps.GetPodInfo(pod)
		if err != nil || podInfo == nil || len(podInfo.Interfaces) == 0 {
			continue
		}
		if err := applyRulesForPod(deps, r.CommonCfg, nil, pod, podInfo, r.HostPrefix); err != nil {
			klog.Errorf("cleanup: failed to remove rules for %s/%s: %v", pod.Namespace, pod.Name, err)
		}
	}
	return nil
}
