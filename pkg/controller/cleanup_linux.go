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

// debugLog writes to stderr (captured in container logs) and a host-mounted
// file for post-mortem debugging of cleanup.
func debugLog(hostPrefix, format string, args ...interface{}) {
	msg := fmt.Sprintf(time.Now().UTC().Format("15:04:05.000")+" "+format+"\n", args...)
	fmt.Fprint(os.Stderr, msg)
	klog.Infof(format, args...)
	path := fmt.Sprintf("%s/tmp/cleanup-debug.log", hostPrefix)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644) //nolint:gosec // debug log path derived from trusted hostPrefix
	if err != nil {
		fmt.Fprintf(os.Stderr, "debugLog: cannot open %s: %v\n", path, err)
		return
	}
	defer f.Close() //nolint:errcheck
	if _, werr := f.WriteString(msg); werr != nil {
		fmt.Fprintf(os.Stderr, "debugLog: write failed: %v\n", werr)
	}
}

func cleanupAllPods(ctx context.Context, r *NodeReconciler, directClient client.Client) error {
	debugLog(r.HostPrefix, "cleanup: starting nftables cleanup for node %s", r.NodeName)
	var podList corev1.PodList
	if err := directClient.List(ctx, &podList, client.MatchingFields{"spec.nodeName": r.NodeName}); err != nil {
		debugLog(r.HostPrefix, "cleanup: failed to list pods: %v", err)
		return err
	}
	debugLog(r.HostPrefix, "cleanup: found %d pods on node %s", len(podList.Items), r.NodeName)
	targeted := 0
	for i := range podList.Items {
		pod := &podList.Items[i]
		if !controllers.IsMultiNetworkpolicyTarget(pod) {
			continue
		}
		targeted++
		netnsPath, err := controllers.GetPodNetNSPath(r.CriClient, pod)
		if err != nil || netnsPath == "" {
			debugLog(r.HostPrefix, "cleanup: pod %s/%s: cannot resolve netns (err=%v), skipping", pod.Namespace, pod.Name, err)
			continue
		}
		debugLog(r.HostPrefix, "cleanup: removing rules for %s/%s (netns=%s)", pod.Namespace, pod.Name, netnsPath)
		if err := flushRulesForPod(pod.Namespace, pod.Name, netnsPath, r.HostPrefix); err != nil {
			debugLog(r.HostPrefix, "cleanup: FAILED to remove rules for %s/%s: %v", pod.Namespace, pod.Name, err)
		} else {
			debugLog(r.HostPrefix, "cleanup: SUCCESS removed rules for %s/%s", pod.Namespace, pod.Name)
		}
	}
	debugLog(r.HostPrefix, "cleanup: finished for node %s (total=%d targeted=%d)", r.NodeName, len(podList.Items), targeted)
	return nil
}
