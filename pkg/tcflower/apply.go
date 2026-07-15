//go:build linux

/*
Copyright 2025 Deutsche Telekom AG.

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

package tcflower

import (
	"context"
	"errors"
	"fmt"
	"syscall"
	"time"

	tc "github.com/florianl/go-tc"
	"github.com/telekom/multi-networkpolicy-nftables/pkg/controllers"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

// Apply enforces the MultiNetworkPolicies that select pod on each of the pod's
// SR-IOV interfaces, using tc flower filters on the VF representors.
//
// For every SR-IOV interface it:
//  1. resolves the VF representor (Phase 1),
//  2. verifies the representor is present / offload-ready (fail closed),
//  3. builds the desired FlowerRules (pure translation),
//  4. ensures a clsact qdisc exists,
//  5. declaratively reconciles installed vs desired filters (add missing,
//     delete stale) — mirroring the nft engine's build-desired-then-diff.
//
// Reconcile keys filters by (parent, priority, handle). SkipSw insertion errors
// are surfaced (fail-closed): a filter that cannot be offloaded to hardware is
// a policy-enforcement failure, not something to swallow.
func Apply(ctx context.Context, deps controllers.PolicyDeps, cfg controllers.CommonRuleConfig,
	policyMap controllers.PolicyMap, pod *corev1.Pod, podInfo *controllers.PodInfo, hostPrefix string) error {
	if podInfo == nil {
		return fmt.Errorf("nil podInfo")
	}

	drv, err := NewDriver()
	if err != nil {
		return fmt.Errorf("open tc driver: %w", err)
	}
	defer func() {
		if cerr := drv.Close(); cerr != nil {
			klog.Errorf("failed to close tc driver: %v", cerr)
		}
	}()

	var errs []error
	for _, iface := range podInfo.Interfaces {
		if !iface.IsSRIOV() {
			continue
		}

		rep, err := ResolveRepresentor(hostPrefix, iface)
		if err != nil {
			// Fail closed: leaving an SR-IOV interface unenforced is a security
			// failure.
			errs = append(errs, fmt.Errorf("resolve representor for interface %q: %w", iface.InterfaceName, err))
			continue
		}

		// Best-effort CT-offload preflight: log (never fail) if SMFS steering
		// mode / ct_max_offloaded_conns cannot be confirmed. See ctPreflight.
		if cfg.CTEnabled {
			ctPreflight(hostPrefix, rep.Name, iface.PCIAddress)
		}
		if err := VerifyOffloadReady(hostPrefix, rep.Name); err != nil {
			errs = append(errs, fmt.Errorf("offload not ready for representor %q: %w", rep.Name, err))
			continue
		}
		if rep.IfIndex == 0 {
			errs = append(errs, fmt.Errorf("representor %q has no resolvable ifindex", rep.Name))
			continue
		}

		// Ensure BuildFlowerRules stamps the resolved representor name onto the
		// rules regardless of whether the CNI populated iface.RepresentorDevice.
		ifaceForBuild := iface
		ifaceForBuild.RepresentorDevice = rep.Name

		desired, err := BuildFlowerRules(ctx, deps, cfg, policyMap, pod, podInfo, ifaceForBuild)
		if err != nil {
			errs = append(errs, fmt.Errorf("build flower rules for representor %q: %w", rep.Name, err))
			continue
		}

		// Observe the per-representor enforcement latency (build+ensure+reconcile)
		// regardless of outcome. Timing starts here — after resolution/preflight,
		// which are gated separately — so the histogram reflects dataplane work.
		start := time.Now()

		if err := drv.EnsureClsact(rep.IfIndex); err != nil {
			observeReconcileDuration(rep.Name, start)
			errs = append(errs, err)
			continue
		}

		if err := reconcile(drv, rep.Name, rep.IfIndex, desired); err != nil {
			observeReconcileDuration(rep.Name, start)
			errs = append(errs, fmt.Errorf("reconcile filters on representor %q: %w", rep.Name, err))
			continue
		}
		observeReconcileDuration(rep.Name, start)
	}

	return errors.Join(errs...)
}

// Flush removes all daemon-managed tc flower filters from the representors of a
// pod's SR-IOV interfaces. It is called on pod delete and daemon shutdown.
//
// It is tolerant of a representor that has already gone away (VF returned to the
// host, node reboot): an unresolvable representor or a missing clsact qdisc is
// treated as "nothing to clean", not an error. Only managed filters (flower +
// SkipSw) are removed; the clsact qdisc itself is left in place because it may
// be shared with other tenants of the same representor.
func Flush(_ context.Context, podInfo *controllers.PodInfo, hostPrefix string) error {
	if podInfo == nil {
		return nil
	}

	drv, err := NewDriver()
	if err != nil {
		return fmt.Errorf("open tc driver: %w", err)
	}
	defer func() {
		if cerr := drv.Close(); cerr != nil {
			klog.Errorf("failed to close tc driver: %v", cerr)
		}
	}()

	var errs []error
	for _, iface := range podInfo.Interfaces {
		if !iface.IsSRIOV() {
			continue
		}

		rep, err := ResolveRepresentor(hostPrefix, iface)
		if err != nil {
			// The representor is gone (VF returned to host, reboot): nothing to
			// clean. Not an error.
			klog.V(4).Infof("tc flush: representor for interface %q unresolved, skipping: %v", iface.InterfaceName, err)
			continue
		}
		if rep.IfIndex == 0 {
			continue
		}
		if err := flushRepresentor(drv, rep.IfIndex); err != nil {
			errs = append(errs, fmt.Errorf("flush filters on representor %q: %w", rep.Name, err))
		}
	}
	return errors.Join(errs...)
}

// flushRepresentor deletes every managed filter on both clsact parents of the
// representor. A representor with no clsact qdisc (ListFilters errors) is
// treated as already-clean.
func flushRepresentor(drv Driver, ifindex int) error {
	var errs []error
	for _, parent := range []uint32{DirIngress.parentHandle(), DirEgress.parentHandle()} {
		objs, err := drv.ListFilters(ifindex, int(parent))
		if err != nil {
			// No clsact qdisc / no filters: nothing to clean on this parent.
			klog.V(4).Infof("tc flush: list filters on ifindex %d parent %#x: %v", ifindex, parent, err)
			continue
		}
		for _, obj := range objs {
			if !isManagedFilter(obj) {
				continue
			}
			if err := drv.DelFilter(obj); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

// filterKey identifies an installed managed filter for diffing.
//
// chain is part of the key because the CT-offload pipeline (Phase 4) installs
// filters in more than one chain per parent: the same numeric priority can be
// reused across chains, so (parent, prio, handle) alone is no longer unique.
// Handles are already chain-salted (see FlowerRule.handle), but keying by chain
// too keeps the diff correct and robust even if two chains ever collide on a
// handle. A chain-0 filter (stateless rules, CT entry chain) has chain==0.
type filterKey struct {
	parent uint32
	chain  uint32
	prio   uint16
	handle uint32
}

// filterChain returns the tc chain of an installed filter, treating an absent
// Chain attribute as chain 0 (the kernel omits it for the default chain).
func filterChain(obj tc.Object) uint32 {
	if obj.Chain != nil {
		return *obj.Chain
	}
	return 0
}

// reconcile installs missing filters and removes stale managed filters on the
// representor, per direction/parent. rep is the representor netdev name, used
// only for metric labels (fail-closed observability).
func reconcile(drv Driver, rep string, ifindex int, desired []FlowerRule) error {
	// Build desired objects keyed by (parent, prio, handle).
	desiredObjs := make(map[filterKey]tc.Object, len(desired))
	parents := make(map[uint32]struct{})
	desiredPerDir := make(map[Direction]int)
	for _, r := range desired {
		obj := r.toObject(ifindex)
		key := filterKey{parent: obj.Parent, chain: r.Chain, prio: r.Priority, handle: obj.Handle}
		desiredObjs[key] = obj
		parents[obj.Parent] = struct{}{}
		desiredPerDir[r.Direction]++
	}
	// Publish the desired filter count per direction. Both directions are set
	// (0 when a direction carries no rules) so removing all policies for a
	// direction is reflected as a drop to zero, not a stale gauge.
	setFiltersInstalled(rep, DirIngress.String(), desiredPerDir[DirIngress])
	setFiltersInstalled(rep, DirEgress.String(), desiredPerDir[DirEgress])

	// Always reconcile both representor parents so that removing all policies
	// tears the previously-installed filters down.
	parents[DirIngress.parentHandle()] = struct{}{}
	parents[DirEgress.parentHandle()] = struct{}{}

	// Gather currently-installed managed filters across the relevant parents.
	installed := make(map[filterKey]tc.Object)
	for parent := range parents {
		objs, err := drv.ListFilters(ifindex, int(parent))
		if err != nil {
			return err
		}
		for _, obj := range objs {
			if !isManagedFilter(obj) {
				continue
			}
			key := filterKey{parent: obj.Parent, chain: filterChain(obj), prio: filterPriority(obj.Info), handle: obj.Handle}
			installed[key] = obj
		}
	}

	// Install every desired filter. AddFilter uses tc Replace, so an already
	// present filter with the same content-derived (parent,prio,handle) is a
	// self-healing no-op; a missing one is created. This is idempotent whether
	// or not the filter was in `installed`.
	for _, obj := range desiredObjs {
		if err := drv.AddFilter(obj); err != nil {
			// Classify the failure for the fail-closed error counter: a hardware
			// offload rejection (skip_sw insertion refused) is the security-
			// critical case and is labeled distinctly from other add failures.
			incFilterApplyError(rep, addErrorReason(err))
			return err
		}
	}

	// Delete stale filters no longer desired.
	for key, obj := range installed {
		if _, ok := desiredObjs[key]; ok {
			continue
		}
		if err := drv.DelFilter(obj); err != nil {
			incFilterApplyError(rep, reasonDelete)
			return err
		}
	}

	return nil
}

// addErrorReason classifies an AddFilter failure for the apply-error counter.
// Every managed filter carries the SkipSw (hardware-only) flag, so the kernel
// rejects an un-offloadable filter rather than falling back to software. That
// rejection surfaces as EOPNOTSUPP/ENOTSUPP from the driver; we label it
// skip_sw (the fail-closed offload-insertion signal). Anything else is a
// generic add failure.
func addErrorReason(err error) string {
	if errors.Is(err, syscall.EOPNOTSUPP) || errors.Is(err, syscall.ENOTSUP) {
		return reasonSkipSw
	}
	return reasonAdd
}

// isManagedFilter reports whether an installed filter is one this backend owns.
// Managed filters are flower filters carrying the SkipSw flag (every filter we
// install sets it); non-flower or software filters are left untouched.
func isManagedFilter(obj tc.Object) bool {
	if obj.Kind != "flower" || obj.Flower == nil {
		return false
	}
	return obj.Flower.Flags != nil && (*obj.Flower.Flags&tc.SkipSw) != 0
}
