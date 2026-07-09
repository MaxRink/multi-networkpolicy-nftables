//go:build linux

package controller

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/telekom/multi-networkpolicy-nftables/pkg/controllers"
	corev1 "k8s.io/api/core/v1"
	pb "k8s.io/cri-api/pkg/apis/runtime/v1"
	klog "k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// debugLog writes to stderr and klog, both of which are captured in container logs.
func debugLog(_ string, format string, args ...interface{}) {
	msg := fmt.Sprintf(time.Now().UTC().Format("15:04:05.000")+" "+format+"\n", args...)
	fmt.Fprint(os.Stderr, msg)
	klog.Infof(format, args...)
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
	var criClient pb.RuntimeServiceClient
	var cleanupErrs []error
	for i := range podList.Items {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("cleanup canceled: %w", err)
		}
		pod := &podList.Items[i]
		if !controllers.IsMultiNetworkpolicyTarget(pod) {
			continue
		}
		targeted++
		if criClient == nil {
			var err error
			criClient, err = r.criRuntimeClient(ctx)
			if err != nil {
				debugLog(r.HostPrefix, "cleanup: failed to connect to CRI: %v", err)
				return fmt.Errorf("cleanup: connect to CRI: %w", err)
			}
		}
		netnsPath, err := controllers.GetPodNetNSPathWithContext(ctx, criClient, pod)
		if err != nil || netnsPath == "" {
			debugLog(r.HostPrefix, "cleanup: pod %s/%s: cannot resolve netns (err=%v), skipping", pod.Namespace, pod.Name, err)
			if err == nil {
				err = fmt.Errorf("empty netns path")
			}
			cleanupErrs = append(cleanupErrs, fmt.Errorf("pod %s/%s: resolve netns: %w", pod.Namespace, pod.Name, err))
			continue
		}
		debugLog(r.HostPrefix, "cleanup: removing rules for %s/%s", pod.Namespace, pod.Name)
		if err := flushRulesForPod(pod.Namespace, pod.Name, netnsPath, r.HostPrefix); err != nil {
			debugLog(r.HostPrefix, "cleanup: FAILED to remove rules for %s/%s: %v", pod.Namespace, pod.Name, err)
			cleanupErrs = append(cleanupErrs, fmt.Errorf("pod %s/%s: flush rules: %w", pod.Namespace, pod.Name, err))
		} else {
			debugLog(r.HostPrefix, "cleanup: SUCCESS removed rules for %s/%s", pod.Namespace, pod.Name)
		}
	}
	debugLog(r.HostPrefix, "cleanup: finished for node %s (total=%d targeted=%d)", r.NodeName, len(podList.Items), targeted)
	if len(cleanupErrs) > 0 {
		return errors.Join(cleanupErrs...)
	}
	return nil
}
