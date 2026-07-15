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
//
// The tc backend runs the stateful conntrack-offload (CT) pipeline by default,
// mirroring the nft backend's always-on allowConntracked behavior (established
// and related return traffic is accepted statefully, so a policy only needs to
// permit the NEW direction). CT is forced on here rather than plumbed through a
// CLI flag to keep the daemon's option surface unchanged for this phase.
func applyTCRulesForPod(ctx context.Context, deps controllers.PolicyDeps, cfg controllers.CommonRuleConfig, policyMap controllers.PolicyMap, pod *v1.Pod, podInfo *controllers.PodInfo, hostPrefix string) error {
	cfg.CTEnabled = true
	return tcflower.Apply(ctx, deps, cfg, policyMap, pod, podInfo, hostPrefix)
}
