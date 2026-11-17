/*
Copyright 2020 The Kubernetes Authors.

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

package server

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"

	multiv1beta1 "github.com/k8snetworkplumbingwg/multi-networkpolicy/pkg/apis/k8s.cni.cncf.io/v1beta1"
	"github.com/telekom/multi-networkpolicy-nftables/pkg/controllers"

	nftables "github.com/google/nftables"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"
)

// Server holds the daemon's shared state that drives health and readiness checks.
//
// All fields are accessed concurrently: the HTTP handlers in HealthServer read
// them on every /readyz request while the informer sync goroutines and the
// shutdown signal handler write them from the daemon's main goroutine tree.
// They must therefore be atomics rather than plain bools.
type Server struct {
	policySynced atomic.Bool
	netdefSynced atomic.Bool
	nsSynced     atomic.Bool
	shuttingDown atomic.Bool
}

// NewServer creates a new Server with all sync state unset (not ready) and not
// shutting down.
func NewServer() *Server {
	return &Server{}
}

// AllSynced reports whether all informer caches have been synced.
func (s *Server) AllSynced() bool {
	return s.policySynced.Load() && s.netdefSynced.Load() && s.nsSynced.Load()
}

// MarkPolicySynced records that the MultiNetworkPolicy informer has completed
// its initial sync.
func (s *Server) MarkPolicySynced() {
	s.policySynced.Store(true)
}

// MarkNetDefSynced records that the NetworkAttachmentDefinition informer has
// completed its initial sync.
func (s *Server) MarkNetDefSynced() {
	s.netdefSynced.Store(true)
}

// MarkNSSynced records that the Namespace informer has completed its initial
// sync.
func (s *Server) MarkNSSynced() {
	s.nsSynced.Store(true)
}

// MarkShuttingDown records that the daemon has begun its shutdown sequence so
// that /readyz starts reporting 503 immediately.
func (s *Server) MarkShuttingDown() {
	s.shuttingDown.Store(true)
}

type internalPolicy struct {
	policy         *multiv1beta1.MultiNetworkPolicy
	policyNetworks []string
	// invalid marks a policy whose rules cannot be built. It still selects
	// the pod, so the pod gets the drop, but none of its rules.
	invalid bool
}

// CompareInternalPolicy orders internal policies by namespace and name.
func CompareInternalPolicy(a, b internalPolicy) int {
	return strings.Compare(fmt.Sprintf("%s/%s", a.policy.GetNamespace(), a.policy.GetName()), fmt.Sprintf("%s/%s", b.policy.GetNamespace(), b.policy.GetName()))
}

// ApplyPolicyRulesForPodAndFamily applies nftables rules for the given pod using the provided deps and config.
func ApplyPolicyRulesForPodAndFamily(ctx context.Context, deps controllers.PolicyDeps, cfg controllers.CommonRuleConfig, policyMap controllers.PolicyMap, pod *v1.Pod, podInfo *controllers.PodInfo, nft *nftables.Conn) error {
	klog.V(4).Infof("Generate rules for Pod: [%s]\n", podNamespacedName(pod))

	nftState, err := bootstrapNetfilterRules(nft, podInfo)
	if err != nil {
		return fmt.Errorf("bootstrap netfilter rules failed for pod [%s]: %w", podNamespacedName(pod), err)
	}
	if nftState == nil {
		return fmt.Errorf("bootstrap netfilter rules returned nil state for pod [%s]", podNamespacedName(pod))
	}

	var ingressPolicies []internalPolicy
	var egressPolicies []internalPolicy
	var invalidErrs []error

	for _, policy := range policyMap {
		if policy.GetNamespace() != pod.Namespace {
			continue
		}
		if policy.Spec.PodSelector.Size() != 0 {
			policyPodSelector, err := metav1.LabelSelectorAsSelector(&policy.Spec.PodSelector)
			if err != nil {
				klog.Errorf("bad label selector for policy [%s]: %v", policyNamespacedName(policy), err)
				continue
			}
			if !policyPodSelector.Matches(labels.Set(pod.Labels)) {
				continue
			}
		}

		ingressEnable, egressEnable := getEnabledPolicyTypes(policy)
		klog.V(8).Infof("ingress/egress = %v/%v\n", ingressEnable, egressEnable)

		policyNetworksAnnot, ok := policy.GetAnnotations()[PolicyNetworkAnnotation]
		if !ok {
			continue
		}
		policyNetworksAnnot = strings.ReplaceAll(policyNetworksAnnot, " ", "")
		policyNetworks := strings.Split(policyNetworksAnnot, ",")
		for pidx, networkName := range policyNetworks {
			if !strings.ContainsAny(networkName, "/") {
				policyNetworks[pidx] = fmt.Sprintf("%s/%s", policy.GetNamespace(), networkName)
			}
		}
		slices.Sort(policyNetworks)

		if podInfo.CheckPolicyNetwork(policyNetworks) {
			ip := internalPolicy{policy: policy, policyNetworks: policyNetworks}
			// Fail closed: an invalid policy would fail the whole batch and
			// leave the pod without any rules, not even the drop.
			if err := validatePolicy(policy); err != nil {
				invalidErrs = append(invalidErrs, fmt.Errorf("invalid policy %q selects the pod but allows nothing: %w", policyNamespacedName(policy), err))
				ip.invalid = true
			}
			if ingressEnable {
				ingressPolicies = append(ingressPolicies, ip)
			}
			if egressEnable {
				egressPolicies = append(egressPolicies, ip)
			}
		}
	}

	err = nftState.applyCommonChainRules(cfg)
	if err != nil {
		return fmt.Errorf("failed to apply common chain rules for pod [%s]: %w", podNamespacedName(pod), err)
	}

	slices.SortStableFunc(ingressPolicies, CompareInternalPolicy)
	slices.SortStableFunc(egressPolicies, CompareInternalPolicy)

	if err := nftState.applyDirectionRules(ctx, deps, cfg, nftState.ingressChain, "ingress", podInfo, ingressPolicies, cfg.DefaultDenyIngress); err != nil {
		return err
	}
	if err := nftState.applyDirectionRules(ctx, deps, cfg, nftState.egressChain, "egress", podInfo, egressPolicies, cfg.DefaultDenyEgress); err != nil {
		return err
	}
	if err := nftState.nft.Flush(); err != nil {
		return fmt.Errorf("nft flush failed for pod [%s]: %w", podNamespacedName(pod), err)
	}

	if err := nftState.cleanup(); err != nil {
		return fmt.Errorf("failed to cleanup nft: %w", err)
	}

	return errors.Join(invalidErrs...)
}

// applyDirectionRules adds the rules of the policies of one direction to its
// chain, followed by the drop of everything they do not allow. Without
// policies, the chain gets the drop only with default deny: an unselected pod
// is then treated like one selected by a policy that allows nothing.
func (n *nftState) applyDirectionRules(ctx context.Context, deps controllers.PolicyDeps, cfg controllers.CommonRuleConfig, chain *nftables.Chain, direction string, podInfo *controllers.PodInfo, policies []internalPolicy, defaultDeny bool) error {
	if len(policies) == 0 && !defaultDeny {
		return nil
	}
	forceUpdate := false
	for _, policy := range policies {
		if policy.invalid {
			continue
		}
		newRules, err := n.applyPodRules(ctx, deps, cfg, chain, podInfo, policy.policy, policy.policyNetworks)
		if err != nil {
			return fmt.Errorf("failed to apply pod %s rules for policy %q: %w", direction, policyNamespacedName(policy.policy), err)
		}
		if newRules {
			forceUpdate = true
		}
		if err := n.applyGeneralMarkCheck(chain, policy.policy); err != nil {
			return fmt.Errorf("failed to apply mark check rule in chain %q: %w", chain.Name, err)
		}
	}
	// A new rule was appended after the existing drop; re-append the drop.
	if err := n.applyDropRemaining(chain, forceUpdate); err != nil {
		return fmt.Errorf("failed to apply drop-remaining %s rules: %w", direction, err)
	}
	return nil
}

func podNamespacedName(o *v1.Pod) string {
	if o == nil {
		return "<nil>"
	}
	return o.GetNamespace() + "/" + o.GetName()
}

func policyNamespacedName(o *multiv1beta1.MultiNetworkPolicy) string {
	if o == nil {
		return "<nil>"
	}
	return o.GetNamespace() + "/" + o.GetName()
}

func getEnabledPolicyTypes(policy *multiv1beta1.MultiNetworkPolicy) (bool, bool) {
	var ingressEnable, egressEnable bool
	if len(policy.Spec.PolicyTypes) > 0 {
		for _, v := range policy.Spec.PolicyTypes {
			if strings.EqualFold(string(v), string(multiv1beta1.PolicyTypeIngress)) {
				ingressEnable = true
			} else if strings.EqualFold(string(v), string(multiv1beta1.PolicyTypeEgress)) {
				egressEnable = true
			}
		}
		return ingressEnable, egressEnable
	}

	return policy.Spec.Ingress != nil, policy.Spec.Egress != nil
}
