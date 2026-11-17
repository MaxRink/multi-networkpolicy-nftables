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
	"slices"
	"testing"

	nftables "github.com/google/nftables"
	"github.com/google/nftables/expr"
	multiv1beta1 "github.com/k8snetworkplumbingwg/multi-networkpolicy/pkg/apis/k8s.cni.cncf.io/v1beta1"
	"github.com/telekom/multi-networkpolicy-nftables/pkg/controllers"
	"github.com/telekom/multi-networkpolicy-nftables/pkg/nftest"
	"golang.org/x/sys/unix"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// TestApplyPolicyRulesPolicyEdit checks that a policy edit updates the sets
// of an applied pod: a changed port, except list and CIDR. Constant sets
// rejected the changed elements, and failed the batch on every reconcile.
func TestApplyPolicyRulesPolicyEdit(t *testing.T) {
	c, newNS := nftest.OpenSystemConn(t, true, DEBUG)
	defer nftest.CleanupSystemConn(t, newNS, DEBUG)
	defer c.FlushRuleset()
	defer c.CloseLasting()
	c.FlushRuleset()
	pod := NewFakePodWithNetAnnotation("testns1", "testpod1", "policy-net-1", "", nil)
	podInfo := &controllers.PodInfo{Name: pod.Name, Namespace: pod.Namespace, Interfaces: []controllers.InterfaceInfo{{
		NetattachName: "testns1/policy-net-1", InterfaceName: "net1", InterfaceType: "macvlan", IPs: []string{"10.1.1.1"}}}}
	for i, step := range []struct {
		port   int32
		cidr   string
		except string
	}{{80, "10.0.0.0/16", "10.0.1.0/24"}, {81, "10.0.0.0/16", "10.0.1.0/24"}, {81, "10.0.0.0/16", "10.0.2.0/24"}, {81, "10.1.0.0/16", "10.1.2.0/24"}} {
		port := intstr.FromInt32(step.port)
		policy := &multiv1beta1.MultiNetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{Namespace: "testns1", Name: "p", Annotations: map[string]string{PolicyNetworkAnnotation: "policy-net-1"}},
			Spec: multiv1beta1.MultiNetworkPolicySpec{
				PolicyTypes: []multiv1beta1.MultiPolicyType{multiv1beta1.PolicyTypeIngress},
				Ingress: []multiv1beta1.MultiNetworkPolicyIngressRule{{
					Ports: []multiv1beta1.MultiNetworkPolicyPort{{Port: &port}},
					From:  []multiv1beta1.MultiNetworkPolicyPeer{{IPBlock: &multiv1beta1.IPBlock{CIDR: step.cidr, Except: []string{step.except}}}},
				}},
			},
		}
		policies := controllers.PolicyMap{types.NamespacedName{Namespace: "testns1", Name: "p"}: policy}
		if err := ApplyPolicyRulesForPodAndFamily(context.Background(), newTestPolicyDeps(), controllers.CommonRuleConfig{}, policies, pod, podInfo, c); err != nil {
			t.Errorf("step %d %+v: ApplyPolicyRulesForPodAndFamily() error = %v", i, step, err)
		}
	}
	table := &nftables.Table{Family: nftables.TableFamilyINet, Name: FilterTableName}
	if got := portIntervals(t, c, findSetByComment(t, c, table, "", "ports_0_tcp")); !slices.Equal(got, [][2]uint16{{81, 81}}) {
		t.Errorf("tcp ports after the edit = %v, want [[81 81]]", got)
	}
}

// TestConstantSetReplaced checks that a ports set an older version created
// constant is replaced on the next apply, so that a policy edit can change
// its elements. A constant set rejected the edit and failed the batch on
// every reconcile until the pod restarted.
func TestConstantSetReplaced(t *testing.T) {
	c, newNS := nftest.OpenSystemConn(t, true, DEBUG)
	defer nftest.CleanupSystemConn(t, newNS, DEBUG)
	defer c.CloseLasting()
	c.FlushRuleset()
	defer c.FlushRuleset()

	nftState, _, _, _, err := prepareEnv(c, false)
	if err != nil {
		t.Fatalf("failed to prepare test env: %s", err.Error())
	}
	chain := nftState.ingressChain

	// What an older version left behind: a constant ports set and its rule.
	portsName := nftNameWithSuffix(chain.Name, "-", fmt.Sprintf("%s-%d", portsChainSuffix, 0))
	portChain := c.AddChain(&nftables.Chain{Name: portsName, Table: chain.Table})
	legacy := nftState.getInetSet(chain, portsName, "tcp")
	legacy.Constant = true
	p80 := intstr.FromInt32(80)
	elements, err := validatePortSpec(multiv1beta1.MultiNetworkPolicyPort{Port: &p80})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.AddSet(legacy, elements); err != nil {
		t.Fatal(err)
	}
	if err := nftState.applyProtoPortsRules(portsName, portChain, "ns/p", legacy, []byte{unix.IPPROTO_TCP}); err != nil {
		t.Fatal(err)
	}
	if err := c.Flush(); err != nil {
		t.Fatalf("flush legacy state: %v", err)
	}

	for _, port := range []int32{81, 82} {
		p := intstr.FromInt32(port)
		if err := nftState.applyPolicyPortsRules(chain.Name, chain, "ns/p", []multiv1beta1.MultiNetworkPolicyPort{{Port: &p}}, 0, namedPortTargets{}); err != nil {
			t.Fatalf("port %d: applyPolicyPortsRules(): %v", port, err)
		}
		if err := c.Flush(); err != nil {
			t.Fatalf("port %d: flush: %v", port, err)
		}
		set := findSetByComment(t, c, chain.Table, "", "ports_0_tcp")
		if set.Constant {
			t.Errorf("port %d: set %s is still constant", port, set.Name)
		}
		if got := portIntervals(t, c, set); !slices.Equal(got, [][2]uint16{{uint16(port), uint16(port)}}) {
			t.Errorf("port %d: tcp ports = %v", port, got)
		}
		rules, err := c.GetRules(chain.Table, portChain)
		if err != nil {
			t.Fatal(err)
		}
		lookups := 0
		for _, r := range rules {
			for _, e := range r.Exprs {
				if l, ok := e.(*expr.Lookup); ok && l.SetName == set.Name {
					lookups++
				}
			}
		}
		if lookups != 1 {
			t.Errorf("port %d: %d rules look up set %s, want 1", port, lookups, set.Name)
		}
	}
}
