/*
Copyright 2026 Deutsche Telekom AG.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"sync"

	"github.com/telekom/multi-networkpolicy-nftables/pkg/controllers"
	"github.com/telekom/multi-networkpolicy-nftables/pkg/tcflower"
	corev1 "k8s.io/api/core/v1"
	klog "k8s.io/klog/v2"
)

// Backend applies MultiNetworkPolicy for a single pod's interfaces using a
// specific dataplane. Two backends coexist in the same daemon, selected per
// interface (see selectBackend / partitionByBackend):
//
//   - nftBackend enforces veth-style CNI interfaces (macvlan, ipvlan, bond) by
//     programming nftables inside the pod network namespace.
//   - tcBackend enforces SR-IOV VF interfaces by programming tc flower filters
//     on the host VF representor (hardware-offloaded on switchdev NICs).
//
// A pod may have interfaces served by different backends; each backend is
// invoked with a PodInfo narrowed to only the interfaces it owns.
type Backend interface {
	// Apply programs the desired policy state for the given pod. podInfo has
	// already been narrowed to the interfaces this backend owns.
	Apply(ctx context.Context, deps controllers.PolicyDeps, cfg controllers.CommonRuleConfig, policyMap controllers.PolicyMap, pod *corev1.Pod, podInfo *controllers.PodInfo, hostPrefix string) error
}

// backendKind identifies which dataplane enforces an interface.
type backendKind int

const (
	backendNFT backendKind = iota
	backendTC
)

// String returns a short label used in logs and wrapped errors.
func (k backendKind) String() string {
	switch k {
	case backendTC:
		return "tc"
	default:
		return "nftables"
	}
}

// selectBackend chooses the dataplane for a single interface. SR-IOV VF
// interfaces (which carry PCI/representor device info from the Multus
// network-status annotation) are enforced on the host via tc flower when the
// tc backend can actually enforce them; every other interface uses the in-netns
// nftables path.
//
// An SR-IOV interface FALLS BACK to nftables (as on the base branch, before the
// tc backend existed) when --enable-tc-backend=false, or when
// tcflower.CheckEnforceable fails: legacy (non-switchdev) eSwitch, no
// representor, not a VF, or --tc-ct-mode=require without confirmed CT offload.
// Each fallback is counted in multinetworkpolicy_tc_backend_fallback_total and
// logged once per VF and reason.
func (r *NodeReconciler) selectBackend(iface controllers.InterfaceInfo) backendKind {
	if !iface.IsSRIOV() {
		return backendNFT
	}
	if r.TCBackendDisabled {
		tcflower.IncBackendFallback(tcflower.FallbackReasonTCDisabled)
		logFallbackOnce(iface, tcflower.FallbackReasonTCDisabled, "--enable-tc-backend=false")
		return backendNFT
	}
	check := tcflower.CheckEnforceable
	if r.TCCheckEnforceableFunc != nil {
		check = r.TCCheckEnforceableFunc
	}
	if err := check(r.HostPrefix, r.CommonCfg, iface); err != nil {
		reason := tcflower.FallbackReason(err)
		tcflower.IncBackendFallback(reason)
		logFallbackOnce(iface, reason, err.Error())
		return backendNFT
	}
	return backendTC
}

// fallbackLogged dedupes fallback logs by VF + reason. Keyed by node-local
// hardware identity (PCI / representor), so it is bounded by the VF count.
var fallbackLogged sync.Map

func logFallbackOnce(iface controllers.InterfaceInfo, reason, detail string) {
	key := iface.PCIAddress + "\x00" + iface.RepresentorDevice + "\x00" + reason
	if _, loaded := fallbackLogged.LoadOrStore(key, struct{}{}); loaded {
		return
	}
	msg := "SR-IOV interface %q (pci=%q representor=%q) enforced by the nftables backend in the pod netns " +
		"instead of tc flower (reason=%s): %s"
	if reason == tcflower.FallbackReasonTCDisabled {
		klog.Infof(msg, iface.InterfaceName, iface.PCIAddress, iface.RepresentorDevice, reason, detail)
		return
	}
	klog.Warningf(msg, iface.InterfaceName, iface.PCIAddress, iface.RepresentorDevice, reason, detail)
}

// partitionByBackend groups a pod's interfaces by the dataplane that must
// enforce them (as chosen by selectBackend), returning one narrowed PodInfo per
// backend that has at least one interface. The returned PodInfos share the
// scalar PodInfo fields (name, namespace, netns, node) and carry only their own
// subset of Interfaces. The map is empty when podInfo has no interfaces.
func partitionByBackend(podInfo *controllers.PodInfo, selectBackend func(controllers.InterfaceInfo) backendKind) map[backendKind]*controllers.PodInfo {
	if podInfo == nil || len(podInfo.Interfaces) == 0 {
		return nil
	}

	grouped := map[backendKind][]controllers.InterfaceInfo{}
	for _, iface := range podInfo.Interfaces {
		kind := selectBackend(iface)
		grouped[kind] = append(grouped[kind], iface)
	}

	out := make(map[backendKind]*controllers.PodInfo, len(grouped))
	for kind, ifaces := range grouped {
		narrowed := *podInfo
		narrowed.Interfaces = ifaces
		out[kind] = &narrowed
	}
	return out
}
