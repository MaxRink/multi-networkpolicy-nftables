//go:build !linux

package controller

import (
	"github.com/telekom/multi-networkpolicy-nftables/pkg/controllers"
	v1 "k8s.io/api/core/v1"
)

func applyRulesForPod(_ controllers.PolicyDeps, _ controllers.CommonRuleConfig, _ controllers.PolicyMap, _ *v1.Pod, _ *controllers.PodInfo, _ string) error {
	return nil
}

func flushRulesForPod(_, _, _, _ string) error {
	return nil
}
