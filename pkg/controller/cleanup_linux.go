//go:build linux

package controller

import (
	"context"

	"github.com/telekom/multi-networkpolicy-nftables/pkg/controllers"
	corev1 "k8s.io/api/core/v1"
	klog "k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func cleanupAllPods(ctx context.Context, r *NodeReconciler, directClient client.Client) error {
	klog.Infof("cleanup: starting nftables cleanup for node %s", r.NodeName)
	var podList corev1.PodList
	if err := directClient.List(ctx, &podList, client.MatchingFields{"spec.nodeName": r.NodeName}); err != nil {
		klog.Errorf("cleanup: failed to list pods: %v", err)
		return err
	}
	klog.Infof("cleanup: found %d pods on node %s", len(podList.Items), r.NodeName)
	resolver := &directNetDefResolver{cl: directClient}
	deps := r.policyDeps()
	for i := range podList.Items {
		pod := &podList.Items[i]
		if !controllers.IsMultiNetworkpolicyTarget(pod) {
			continue
		}
		podInfo := controllers.NewPodInfoFromPod(pod, r.CriClient, r.NodeName, r.NetworkPlugins, resolver)
		if podInfo == nil || len(podInfo.Interfaces) == 0 {
			continue
		}
		klog.V(4).Infof("cleanup: removing rules for %s/%s", pod.Namespace, pod.Name)
		if err := applyRulesForPod(deps, r.CommonCfg, nil, pod, podInfo, r.HostPrefix); err != nil {
			klog.Errorf("cleanup: failed to remove rules for %s/%s: %v", pod.Namespace, pod.Name, err)
		}
	}
	klog.Infof("cleanup: finished nftables cleanup for node %s", r.NodeName)
	return nil
}
