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

package server

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	multiv1beta1 "github.com/k8snetworkplumbingwg/multi-networkpolicy/pkg/apis/k8s.cni.cncf.io/v1beta1"
	"github.com/telekom/multi-networkpolicy-nftables/pkg/controllers"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/telekom/multi-networkpolicy-nftables/pkg/nftest"
)

func defaultDenyPod(namespace string) (*controllers.PodInfo, func() *multiv1beta1.MultiNetworkPolicy) {
	podInfo := &controllers.PodInfo{
		Name:      "testpod1",
		Namespace: namespace,
		Interfaces: []controllers.InterfaceInfo{{
			NetattachName: namespace + "/policy-net-1",
			InterfaceName: "net1",
			InterfaceType: "macvlan",
			IPs:           []string{"10.1.1.1"},
		}},
	}
	port := intstr.FromInt32(80)
	policy := func() *multiv1beta1.MultiNetworkPolicy {
		return &multiv1beta1.MultiNetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:   namespace,
				Name:        "allow-80",
				Annotations: map[string]string{PolicyNetworkAnnotation: "policy-net-1"},
			},
			Spec: multiv1beta1.MultiNetworkPolicySpec{
				PolicyTypes: []multiv1beta1.MultiPolicyType{multiv1beta1.PolicyTypeIngress},
				Ingress: []multiv1beta1.MultiNetworkPolicyIngressRule{{
					Ports: []multiv1beta1.MultiNetworkPolicyPort{{Port: &port}},
				}},
			},
		}
	}
	return podInfo, policy
}

// TestApplyPolicyRulesDefaultDeny checks the rules each --default-deny mode
// generates for a pod that no policy selects.
func TestApplyPolicyRulesDefaultDeny(t *testing.T) {
	in := []string{"common-ingress-chain"}
	inDeny := []string{"common-ingress-chain", "drop-remaining"}
	out := []string{"common-egress-chain"}
	outDeny := []string{"common-egress-chain", "drop-remaining"}

	tests := []struct {
		mode    string
		cfg     controllers.CommonRuleConfig
		ingress []string
		egress  []string
	}{
		{mode: "none", ingress: in, egress: out},
		{mode: "ingress", cfg: controllers.CommonRuleConfig{DefaultDenyIngress: true}, ingress: inDeny, egress: out},
		{mode: "egress", cfg: controllers.CommonRuleConfig{DefaultDenyEgress: true}, ingress: in, egress: outDeny},
		{mode: "all", cfg: controllers.CommonRuleConfig{DefaultDenyIngress: true, DefaultDenyEgress: true}, ingress: inDeny, egress: outDeny},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			c, newNS := nftest.OpenSystemConn(t, true, DEBUG)
			defer nftest.CleanupSystemConn(t, newNS, DEBUG)
			defer c.FlushRuleset()
			defer c.CloseLasting()
			c.FlushRuleset()

			podInfo, _ := defaultDenyPod("testns1")
			pod := NewFakePodWithNetAnnotation("testns1", "testpod1", "policy-net-1", "", nil)
			if err := ApplyPolicyRulesForPodAndFamily(context.Background(), newTestPolicyDeps(), tt.cfg, controllers.PolicyMap{}, pod, podInfo, c); err != nil {
				t.Fatalf("ApplyPolicyRulesForPodAndFamily() failed: %v", err)
			}

			if got := chainComments(t, c, ingressChain); !reflect.DeepEqual(got, tt.ingress) {
				t.Errorf("%s rules = %q, want %q", ingressChain, got, tt.ingress)
			}
			if got := chainComments(t, c, egressChain); !reflect.DeepEqual(got, tt.egress) {
				t.Errorf("%s rules = %q, want %q", egressChain, got, tt.egress)
			}
			// What stays allowed under default deny lives in the common
			// chains, which every mode keeps: established/related and ND.
			for _, chain := range []string{ingressChain + "-" + common, egressChain + "-" + common} {
				got := strings.Join(chainComments(t, c, chain), ",")
				for _, want := range []string{"allow-ipv6-ndp-discovery", allowConntrackRuleName} {
					if !strings.Contains(got, want) {
						t.Errorf("%s rules = %q, want %q", chain, got, want)
					}
				}
			}
		})
	}
}

// TestApplyPolicyRulesDefaultDenyPolicyIsAdditive checks that a policy that
// selects a pod under default deny opens what it allows, ahead of the drop,
// and that removing it restores the plain deny.
func TestApplyPolicyRulesDefaultDenyPolicyIsAdditive(t *testing.T) {
	c, newNS := nftest.OpenSystemConn(t, true, DEBUG)
	defer nftest.CleanupSystemConn(t, newNS, DEBUG)
	defer c.FlushRuleset()
	defer c.CloseLasting()
	c.FlushRuleset()

	const namespace = "testns1"
	podInfo, newPolicy := defaultDenyPod(namespace)
	pod := NewFakePodWithNetAnnotation(namespace, "testpod1", "policy-net-1", "", nil)
	cfg := controllers.CommonRuleConfig{DefaultDenyIngress: true}
	apply := func(policies controllers.PolicyMap) []string {
		t.Helper()
		if err := ApplyPolicyRulesForPodAndFamily(context.Background(), newTestPolicyDeps(), cfg, policies, pod, podInfo, c); err != nil {
			t.Fatalf("ApplyPolicyRulesForPodAndFamily() failed: %v", err)
		}
		return chainComments(t, c, ingressChain)
	}

	if got, want := apply(controllers.PolicyMap{}), []string{"common-ingress-chain", "drop-remaining"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("without policy: %s rules = %q, want %q", ingressChain, got, want)
	}

	policy := newPolicy()
	got := apply(controllers.PolicyMap{types.NamespacedName{Namespace: namespace, Name: policy.Name}: policy})
	if len(got) != 4 || got[0] != "common-ingress-chain" || got[3] != "drop-remaining" ||
		!strings.Contains(got[1], "policy:"+namespace+"/allow-80") || !strings.Contains(got[2], "check mark") {
		t.Fatalf("with policy: %s rules = %q, want common jump, policy jump, mark check, drop-remaining", ingressChain, got)
	}

	if got, want := apply(controllers.PolicyMap{}), []string{"common-ingress-chain", "drop-remaining"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after policy removal: %s rules = %q, want %q", ingressChain, got, want)
	}
}

func TestOptionsValidateDefaultDeny(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		value           string
		wantErr         bool
		ingress, egress bool
	}{
		{value: "none"},
		{value: "ingress", ingress: true},
		{value: "egress", egress: true},
		{value: "all", ingress: true, egress: true},
		{value: ""},
		{value: "Ingress", wantErr: true},
		{value: "both", wantErr: true},
	} {
		t.Run(fmt.Sprintf("%q", tt.value), func(t *testing.T) {
			t.Parallel()

			opts := NewOptions()
			opts.containerRuntimeEndpoint = "/run/crio/crio.sock"
			opts.hostnameOverride = "node-1"
			opts.defaultDeny = tt.value

			cfg, err := opts.BuildReconcilerConfig()
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "default-deny") {
					t.Fatalf("BuildReconcilerConfig() error = %v, want default-deny error", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("BuildReconcilerConfig() error = %v", err)
			}
			if cfg.CommonRuleConfig.DefaultDenyIngress != tt.ingress || cfg.CommonRuleConfig.DefaultDenyEgress != tt.egress {
				t.Fatalf("default deny ingress/egress = %v/%v, want %v/%v", cfg.CommonRuleConfig.DefaultDenyIngress, cfg.CommonRuleConfig.DefaultDenyEgress, tt.ingress, tt.egress)
			}
		})
	}
}

// TestApplyPolicyRulesInvalidPolicyFailsClosed checks that a policy whose
// rules cannot be built still isolates the pods it selects, from the first
// apply on, instead of failing the batch and leaving the pod without rules.
func TestApplyPolicyRulesInvalidPolicyFailsClosed(t *testing.T) {
	for name, breakPolicy := range map[string]func(*multiv1beta1.MultiNetworkPolicy){
		"port out of range": func(p *multiv1beta1.MultiNetworkPolicy) {
			outOfRange := intstr.FromInt32(70000)
			p.Spec.Ingress[0].Ports[0].Port = &outOfRange
		},
		"invalid cidr": func(p *multiv1beta1.MultiNetworkPolicy) {
			p.Spec.Ingress[0].From = []multiv1beta1.MultiNetworkPolicyPeer{{IPBlock: &multiv1beta1.IPBlock{CIDR: "10.0.0.0/33"}}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			c, newNS := nftest.OpenSystemConn(t, true, DEBUG)
			defer nftest.CleanupSystemConn(t, newNS, DEBUG)
			defer c.FlushRuleset()
			defer c.CloseLasting()
			c.FlushRuleset()

			podInfo, newPolicy := defaultDenyPod("testns1")
			pod := NewFakePodWithNetAnnotation("testns1", "testpod1", "policy-net-1", "", nil)
			policy := newPolicy()
			breakPolicy(policy)
			policies := controllers.PolicyMap{types.NamespacedName{Namespace: "testns1", Name: policy.Name}: policy}
			err := ApplyPolicyRulesForPodAndFamily(context.Background(), newTestPolicyDeps(), controllers.CommonRuleConfig{}, policies, pod, podInfo, c)
			if err == nil || !strings.Contains(err.Error(), "invalid policy") {
				t.Errorf("ApplyPolicyRulesForPodAndFamily() error = %v, want invalid policy error", err)
			}
			if got, want := chainComments(t, c, "input"), []string{inputInterfaceFilterComment}; !reflect.DeepEqual(got, want) {
				t.Errorf("input rules = %q, want %q", got, want)
			}
			if got, want := chainComments(t, c, ingressChain), []string{"common-ingress-chain", "drop-remaining"}; !reflect.DeepEqual(got, want) {
				t.Errorf("%s rules = %q, want %q", ingressChain, got, want)
			}
		})
	}
}

// TestApplyPolicyRulesNamedPortPolicyIsValid checks that a named port does not
// make a policy invalid: it resolves against container ports, and one that
// matches none allows nothing, but the policy's chain is still built.
func TestApplyPolicyRulesNamedPortPolicyIsValid(t *testing.T) {
	c, newNS := nftest.OpenSystemConn(t, true, DEBUG)
	defer nftest.CleanupSystemConn(t, newNS, DEBUG)
	defer c.FlushRuleset()
	defer c.CloseLasting()
	c.FlushRuleset()

	podInfo, newPolicy := defaultDenyPod("testns2")
	pod := NewFakePodWithNetAnnotation("testns2", "testpod1", "policy-net-1", "", nil)
	policy := newPolicy()
	named := intstr.FromString("http")
	policy.Spec.Ingress[0].Ports[0].Port = &named
	if err := validatePolicy(policy); err != nil {
		t.Fatalf("validatePolicy() error = %v, want nil", err)
	}
	policies := controllers.PolicyMap{types.NamespacedName{Namespace: "testns2", Name: policy.Name}: policy}
	if err := ApplyPolicyRulesForPodAndFamily(context.Background(), newTestPolicyDeps(), controllers.CommonRuleConfig{}, policies, pod, podInfo, c); err != nil {
		t.Fatalf("ApplyPolicyRulesForPodAndFamily() error = %v, want nil", err)
	}
	if got := chainComments(t, c, ingressChain); !slices.ContainsFunc(got, func(c string) bool { return strings.HasPrefix(c, "policy:testns2/allow-80,") }) {
		t.Errorf("%s rules = %q, want the jump to the policy chain", ingressChain, got)
	}
}

// TestApplyPolicyRulesOverlappingPortsFirstApply checks that a valid policy
// with overlapping port ranges builds on the first apply to a pod. Unmerged,
// the interval set rejected them, and the failed batch also dropped the
// interface jumps and the default-deny drops, leaving the pod open.
func TestApplyPolicyRulesOverlappingPortsFirstApply(t *testing.T) {
	c, newNS := nftest.OpenSystemConn(t, true, DEBUG)
	defer nftest.CleanupSystemConn(t, newNS, DEBUG)
	defer c.FlushRuleset()
	defer c.CloseLasting()
	c.FlushRuleset()

	podInfo, newPolicy := defaultDenyPod("testns1")
	pod := NewFakePodWithNetAnnotation("testns1", "testpod1", "policy-net-1", "", nil)
	policy := newPolicy()
	p80, p85 := intstr.FromInt32(80), intstr.FromInt32(85)
	end90, end95 := int32(90), int32(95)
	policy.Spec.Ingress[0].Ports = []multiv1beta1.MultiNetworkPolicyPort{{Port: &p80, EndPort: &end90}, {Port: &p85, EndPort: &end95}}
	if err := validatePolicy(policy); err != nil {
		t.Fatalf("validatePolicy() error = %v, want nil", err)
	}
	policies := controllers.PolicyMap{types.NamespacedName{Namespace: "testns1", Name: policy.Name}: policy}
	cfg := controllers.CommonRuleConfig{DefaultDenyIngress: true, DefaultDenyEgress: true}
	if err := ApplyPolicyRulesForPodAndFamily(context.Background(), newTestPolicyDeps(), cfg, policies, pod, podInfo, c); err != nil {
		t.Fatalf("ApplyPolicyRulesForPodAndFamily() error = %v, want nil", err)
	}

	if got, want := chainComments(t, c, "input"), []string{inputInterfaceFilterComment}; !reflect.DeepEqual(got, want) {
		t.Errorf("input rules = %q, want %q", got, want)
	}
	got := chainComments(t, c, ingressChain)
	if len(got) != 4 || got[0] != "common-ingress-chain" || got[3] != "drop-remaining" || !strings.Contains(got[1], "policy:testns1/allow-80") {
		t.Errorf("%s rules = %q, want common jump, policy jump, mark check, drop-remaining", ingressChain, got)
	}
	if got, want := chainComments(t, c, egressChain), []string{"common-egress-chain", "drop-remaining"}; !reflect.DeepEqual(got, want) {
		t.Errorf("%s rules = %q, want %q", egressChain, got, want)
	}
}
