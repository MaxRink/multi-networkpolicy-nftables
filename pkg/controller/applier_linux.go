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

	isCleanup := policyMap == nil
	if isCleanup {
		debugDumpNftState(hostPrefix, "BEFORE-CLEANUP", pod, nft)
	}
	applyErr := server.ApplyPolicyRulesForPodAndFamily(deps, cfg, policyMap, pod, podInfo, nft)
	if isCleanup {
		debugDumpNftState(hostPrefix, "AFTER-CLEANUP", pod, nft)
	}
	return applyErr
}

func debugDumpNftState(hostPrefix, label string, pod *v1.Pod, nft *nftables.Conn) {
	tables, err := nft.ListTables()
	if err != nil {
		debugLog(hostPrefix, "nft-debug %s %s/%s: ListTables error: %v", label, pod.Namespace, pod.Name, err)
		return
	}
	for _, t := range tables {
		chains, _ := nft.ListChainsOfTableFamily(t.Family)
		for _, c := range chains {
			if c.Table.Name != t.Name {
				continue
			}
			rules, _ := nft.GetRules(t, c)
			debugLog(hostPrefix, "nft-debug %s %s/%s: table=%s chain=%s rules=%d", label, pod.Namespace, pod.Name, t.Name, c.Name, len(rules))
		}
	}
}
