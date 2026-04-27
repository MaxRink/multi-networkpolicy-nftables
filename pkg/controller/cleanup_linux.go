//go:build linux

package controller

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/telekom/multi-networkpolicy-nftables/pkg/controllers"
	corev1 "k8s.io/api/core/v1"
	klog "k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// debugLog writes to a host-mounted file for post-mortem debugging of cleanup.
// The file persists after pod deletion because it's on the host filesystem.
func debugLog(hostPrefix, format string, args ...interface{}) {
	msg := fmt.Sprintf(time.Now().UTC().Format("15:04:05.000")+" "+format+"\n", args...)
	klog.Infof(format, args...)
	path := fmt.Sprintf("%s/tmp/cleanup-debug.log", hostPrefix)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644) //nolint:gosec // debug log path derived from trusted hostPrefix
	if err != nil {
		return
	}
	defer f.Close() //nolint:errcheck
	_, _ = f.WriteString(msg)
}

func cleanupAllPods(ctx context.Context, r *NodeReconciler, directClient client.Client) error {
	debugLog(r.HostPrefix, "cleanup: starting nftables cleanup for node %s", r.NodeName)
	var podList corev1.PodList
	if err := directClient.List(ctx, &podList, client.MatchingFields{"spec.nodeName": r.NodeName}); err != nil {
		debugLog(r.HostPrefix, "cleanup: failed to list pods: %v", err)
		return err
	}
	debugLog(r.HostPrefix, "cleanup: found %d pods on node %s", len(podList.Items), r.NodeName)
	resolver := &directNetDefResolver{cl: directClient}
	targeted := 0
	for i := range podList.Items {
		pod := &podList.Items[i]
		if !controllers.IsMultiNetworkpolicyTarget(pod) {
			continue
		}
		targeted++
		podInfo := controllers.NewPodInfoFromPod(pod, r.CriClient, r.NodeName, r.NetworkPlugins, resolver)
		if podInfo == nil {
			debugLog(r.HostPrefix, "cleanup: pod %s/%s podInfo=nil, skipping", pod.Namespace, pod.Name)
			continue
		}
		if len(podInfo.Interfaces) == 0 {
			debugLog(r.HostPrefix, "cleanup: pod %s/%s interfaces=0 netns=%q, skipping", pod.Namespace, pod.Name, podInfo.NetNSPath)
			continue
		}
		debugLog(r.HostPrefix, "cleanup: removing rules for %s/%s (netns=%s, ifaces=%d)", pod.Namespace, pod.Name, podInfo.NetNSPath, len(podInfo.Interfaces))
		if err := flushRulesForPod(pod, podInfo, r.HostPrefix); err != nil {
			debugLog(r.HostPrefix, "cleanup: FAILED to remove rules for %s/%s: %v", pod.Namespace, pod.Name, err)
		} else {
			debugLog(r.HostPrefix, "cleanup: SUCCESS removed rules for %s/%s", pod.Namespace, pod.Name)
		}
	}
	debugLog(r.HostPrefix, "cleanup: finished for node %s (total=%d targeted=%d)", r.NodeName, len(podList.Items), targeted)
	return nil
}
