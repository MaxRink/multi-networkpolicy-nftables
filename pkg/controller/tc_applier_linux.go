//go:build linux

package controller

import (
	"context"
	"fmt"

	"github.com/telekom/multi-networkpolicy-nftables/pkg/controllers"
	v1 "k8s.io/api/core/v1"
)

// applyTCRulesForPod enforces a pod's SR-IOV VF interfaces by programming tc
// flower filters on the host VF representor. The translation engine and tc
// driver land in a later phase; until then this returns a clear error so
// SR-IOV interfaces are never silently left unenforced.
func applyTCRulesForPod(_ context.Context, _ controllers.PolicyDeps, _ controllers.CommonRuleConfig, _ controllers.PolicyMap, pod *v1.Pod, _ *controllers.PodInfo, _ string) error {
	return fmt.Errorf("tc flower enforcement for SR-IOV pod %s/%s is not yet implemented", pod.Namespace, pod.Name)
}
