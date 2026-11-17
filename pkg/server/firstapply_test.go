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
	"reflect"
	"testing"

	nftables "github.com/google/nftables"
	"github.com/google/nftables/userdata"
	"github.com/telekom/multi-networkpolicy-nftables/pkg/controllers"
	"github.com/telekom/multi-networkpolicy-nftables/pkg/nftest"
)

// chainComments returns the comments of the rules in a chain of the filter
// table, in rule order.
func chainComments(t *testing.T, c *nftables.Conn, chain string) []string {
	t.Helper()
	table := &nftables.Table{Family: nftables.TableFamilyINet, Name: FilterTableName}
	rules, err := c.GetRules(table, &nftables.Chain{Name: chain, Table: table})
	if err != nil {
		t.Fatalf("GetRules(%s) failed: %v", chain, err)
	}
	comments := make([]string, 0, len(rules))
	for _, r := range rules {
		comment, _ := userdata.GetString(r.UserData, userdata.TypeComment)
		comments = append(comments, comment)
	}
	return comments
}

// TestApplyPolicyRulesFirstApplyKeepsSetLookups checks that the rules that
// look up a set created in the same batch survive the cleanup of the first
// apply to a pod: the input and output jumps into the pod chains (set
// pod_interfaces) and the IPv6 neighbor discovery rules (set ndp_set).
// Without them, a new pod is unenforced until the next reconcile.
func TestApplyPolicyRulesFirstApplyKeepsSetLookups(t *testing.T) {
	c, newNS := nftest.OpenSystemConn(t, true, DEBUG)
	defer nftest.CleanupSystemConn(t, newNS, DEBUG)
	defer c.FlushRuleset()
	defer c.CloseLasting()
	c.FlushRuleset()

	pod := NewFakePodWithNetAnnotation("testns1", "testpod1", "policy-net-1", "", nil)
	podInfo := &controllers.PodInfo{
		Name:      pod.Name,
		Namespace: pod.Namespace,
		Interfaces: []controllers.InterfaceInfo{{
			NetattachName: "testns1/policy-net-1",
			InterfaceName: "net1",
			InterfaceType: "macvlan",
			IPs:           []string{"10.1.1.1"},
		}},
	}
	if err := ApplyPolicyRulesForPodAndFamily(context.Background(), newTestPolicyDeps(), controllers.CommonRuleConfig{}, controllers.PolicyMap{}, pod, podInfo, c); err != nil {
		t.Fatalf("ApplyPolicyRulesForPodAndFamily() failed: %v", err)
	}

	for chain, want := range map[string][]string{
		"input":                     {inputInterfaceFilterComment},
		"output":                    {outputInterfaceFilterComment},
		ingressChain + "-" + common: {"allow-ipv6-ndp-discovery", allowConntrackRuleName},
		egressChain + "-" + common:  {"allow-ipv6-ndp-discovery", allowConntrackRuleName},
	} {
		if got := chainComments(t, c, chain); !reflect.DeepEqual(got, want) {
			t.Errorf("%s rules after the first apply = %q, want %q", chain, got, want)
		}
	}
}
