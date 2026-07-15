//go:build linux

package controller

import (
	"context"

	"github.com/telekom/multi-networkpolicy-nftables/pkg/controllers"
	"github.com/telekom/multi-networkpolicy-nftables/pkg/tcflower"
	v1 "k8s.io/api/core/v1"
)

// applyTCRulesForPod enforces a pod's SR-IOV VF interfaces by programming tc
// flower filters on the host VF representor (hardware-offloaded on switchdev
// NICs). podInfo has already been narrowed to the pod's SR-IOV interfaces by
// the reconciler's per-interface backend partitioning.
func applyTCRulesForPod(ctx context.Context, deps controllers.PolicyDeps, cfg controllers.CommonRuleConfig, policyMap controllers.PolicyMap, pod *v1.Pod, podInfo *controllers.PodInfo, hostPrefix string) error {
	return tcflower.Apply(ctx, deps, cfg, policyMap, pod, podInfo, hostPrefix)
}
