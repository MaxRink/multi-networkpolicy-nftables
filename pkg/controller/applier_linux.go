//go:build linux

package controller

import (
	"fmt"
	"math"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/google/nftables"
	"github.com/telekom/multi-networkpolicy-nftables/pkg/controllers"
	"github.com/telekom/multi-networkpolicy-nftables/pkg/server"
	v1 "k8s.io/api/core/v1"
	klog "k8s.io/klog/v2"
)

func applyRulesForPod(deps controllers.PolicyDeps, cfg controllers.CommonRuleConfig, policyMap controllers.PolicyMap, pod *v1.Pod, podInfo *controllers.PodInfo, hostPrefix string) error {
	netnsPath := podInfo.NetNSPath
	if hostPrefix != "" {
		netnsPath = fmt.Sprintf("%s/%s", hostPrefix, netnsPath)
	}
	netNs, err := ns.GetNS(netnsPath)
	if err != nil {
		return fmt.Errorf("cannot get pod (%s/%s) netns (%s): %w", pod.Namespace, pod.Name, netnsPath, err)
	}
	defer func() {
		if cerr := netNs.Close(); cerr != nil {
			klog.Errorf("cannot close pod (%s/%s) netns: %v", pod.Namespace, pod.Name, cerr)
		}
	}()
	fd := netNs.Fd()
	if fd > uintptr(math.MaxInt) {
		return fmt.Errorf("netns fd %d overflows int", fd)
	}
	nft, err := nftables.New(nftables.WithNetNSFd(int(fd)), nftables.AsLasting())
	if err != nil {
		return fmt.Errorf("failed to open nftables for pod (%s/%s): %w", pod.Namespace, pod.Name, err)
	}
	defer func() {
		if cerr := nft.CloseLasting(); cerr != nil {
			klog.Errorf("failed to close nftables for pod (%s/%s): %v", pod.Namespace, pod.Name, cerr)
		}
	}()

	return server.ApplyPolicyRulesForPodAndFamily(deps, cfg, policyMap, pod, podInfo, nft)
}

// flushRulesForPod removes all nftables tables from the pod's network namespace.
// It enters the pod namespace at the OS thread level via Do() which calls
// runtime.LockOSThread() + setns(). A plain nftables.New() inside Do()
// creates a netlink socket that is bound to the current (pod) namespace.
//
// The previous approach used nftables.WithNetNSFd(fd) which only sets the
// namespace fd on the netlink socket WITHOUT calling setns(). During
// DaemonSet shutdown this silently operated on the wrong namespace —
// DelTable/Flush returned nil and ListTables returned 0, but rules
// persisted in the pod netns.
func flushRulesForPod(podNamespace, podName, netnsPath, hostPrefix string) error {
	if hostPrefix != "" {
		netnsPath = fmt.Sprintf("%s/%s", hostPrefix, netnsPath)
	}
	podNs, err := ns.GetNS(netnsPath)
	if err != nil {
		return fmt.Errorf("cannot get pod (%s/%s) netns (%s): %w", podNamespace, podName, netnsPath, err)
	}
	defer func() {
		if cerr := podNs.Close(); cerr != nil {
			klog.Errorf("cannot close pod (%s/%s) netns: %v", podNamespace, podName, cerr)
		}
	}()

	return podNs.Do(func(_ ns.NetNS) error {
		nft, nftErr := nftables.New()
		if nftErr != nil {
			return fmt.Errorf("failed to open nftables for pod (%s/%s): %w", podNamespace, podName, nftErr)
		}
		tables, listErr := nft.ListTables()
		if listErr != nil {
			return fmt.Errorf("failed to list tables for pod (%s/%s): %w", podNamespace, podName, listErr)
		}
		if len(tables) == 0 {
			debugLog(hostPrefix, "flush-cleanup %s/%s: no tables found, nothing to clean", podNamespace, podName)
			return nil
		}
		debugLog(hostPrefix, "flush-cleanup %s/%s: deleting %d tables", podNamespace, podName, len(tables))
		for _, t := range tables {
			debugLog(hostPrefix, "flush-cleanup %s/%s: DelTable %s (family=%d)", podNamespace, podName, t.Name, t.Family)
			nft.DelTable(t)
		}
		if flushErr := nft.Flush(); flushErr != nil {
			return fmt.Errorf("failed to flush table deletions for pod (%s/%s): %w", podNamespace, podName, flushErr)
		}
		tablesAfter, _ := nft.ListTables()
		debugLog(hostPrefix, "flush-cleanup %s/%s: %d tables remaining after flush", podNamespace, podName, len(tablesAfter))
		return nil
	})
}
