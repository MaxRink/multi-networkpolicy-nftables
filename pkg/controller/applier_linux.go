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
// It uses DelTable (which removes the table along with all chains/rules) and
// avoids AsLasting() — the lasting netlink connection has proven unreliable
// during shutdown, where Flush() returns nil but changes don't persist.
func flushRulesForPod(pod *v1.Pod, podInfo *controllers.PodInfo, hostPrefix string) error {
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
	// No AsLasting() — each Flush() creates a fresh netlink connection,
	// avoiding the silent-failure bug observed with lasting connections
	// during DaemonSet shutdown.
	nft, err := nftables.New(nftables.WithNetNSFd(int(fd)))
	if err != nil {
		return fmt.Errorf("failed to open nftables for pod (%s/%s): %w", pod.Namespace, pod.Name, err)
	}
	tables, err := nft.ListTables()
	if err != nil {
		return fmt.Errorf("failed to list tables for pod (%s/%s): %w", pod.Namespace, pod.Name, err)
	}
	if len(tables) == 0 {
		debugLog(hostPrefix, "flush-cleanup %s/%s: no tables found, nothing to clean", pod.Namespace, pod.Name)
		return nil
	}
	debugLog(hostPrefix, "flush-cleanup %s/%s: deleting %d tables", pod.Namespace, pod.Name, len(tables))
	for _, t := range tables {
		debugLog(hostPrefix, "flush-cleanup %s/%s: DelTable %s (family=%d)", pod.Namespace, pod.Name, t.Name, t.Family)
		nft.DelTable(t)
	}
	if err := nft.Flush(); err != nil {
		return fmt.Errorf("failed to flush table deletions for pod (%s/%s): %w", pod.Namespace, pod.Name, err)
	}
	tablesAfter, _ := nft.ListTables()
	debugLog(hostPrefix, "flush-cleanup %s/%s: %d tables remaining after flush", pod.Namespace, pod.Name, len(tablesAfter))
	return nil
}
