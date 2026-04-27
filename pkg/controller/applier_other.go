//go:build !linux

package controller

import (
	"context"
	"fmt"
	"runtime"

	"github.com/telekom/multi-networkpolicy-nftables/pkg/controllers"
	v1 "k8s.io/api/core/v1"
)

func applyRulesForPod(_ context.Context, _ controllers.PolicyDeps, _ controllers.CommonRuleConfig, _ controllers.PolicyMap, _ *v1.Pod, _ *controllers.PodInfo, _ string) error {
	return fmt.Errorf("nftables rule application is unsupported on %s", runtime.GOOS)
}

func flushRulesForPod(_ *v1.Pod, _ *controllers.PodInfo, _ string) error {
	return nil
}
