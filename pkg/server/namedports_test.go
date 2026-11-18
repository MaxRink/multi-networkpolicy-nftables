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
	"bytes"
	"context"
	"fmt"
	"io"
	"maps"
	"net/netip"
	"slices"
	"strings"
	"testing"

	nftables "github.com/google/nftables"
	"github.com/google/nftables/binaryutil"
	multiv1beta1 "github.com/k8snetworkplumbingwg/multi-networkpolicy/pkg/apis/k8s.cni.cncf.io/v1beta1"
	"github.com/telekom/multi-networkpolicy-nftables/pkg/controllers"
	"github.com/telekom/multi-networkpolicy-nftables/pkg/nftest"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/klog/v2"
)

func TestResolveNamedPort(t *testing.T) {
	t.Parallel()
	ports := []corev1.ContainerPort{
		{Name: "http", ContainerPort: 8080}, // protocol defaults to TCP
		{Name: "dns", ContainerPort: 53, Protocol: corev1.ProtocolUDP},
		{Name: "dns", ContainerPort: 5353, Protocol: corev1.ProtocolTCP},
	}
	cases := []struct {
		name   string
		proto  corev1.Protocol
		want   uint16
		wantOK bool
	}{
		{"http", corev1.ProtocolTCP, 8080, true},
		{"http", corev1.ProtocolUDP, 0, false},
		{"dns", corev1.ProtocolUDP, 53, true},
		{"dns", corev1.ProtocolTCP, 5353, true},
		{"missing", corev1.ProtocolTCP, 0, false},
	}
	for _, tc := range cases {
		got, ok := resolveNamedPort(ports, tc.name, tc.proto)
		if got != tc.want || ok != tc.wantOK {
			t.Errorf("resolveNamedPort(%q, %s) = %d, %v; want %d, %v", tc.name, tc.proto, got, ok, tc.want, tc.wantOK)
		}
	}
}

func TestNamedPortName(t *testing.T) {
	t.Parallel()
	http, num, numInt := intstr.FromString("http"), intstr.FromString("8080"), intstr.FromInt32(80)
	cases := []struct {
		port multiv1beta1.MultiNetworkPolicyPort
		want string
	}{
		{multiv1beta1.MultiNetworkPolicyPort{}, ""},
		{multiv1beta1.MultiNetworkPolicyPort{Port: &numInt}, ""},
		{multiv1beta1.MultiNetworkPolicyPort{Port: &num}, ""},
		{multiv1beta1.MultiNetworkPolicyPort{Port: &http}, "http"},
	}
	for _, tc := range cases {
		if got := namedPortName(tc.port); got != tc.want {
			t.Errorf("namedPortName(%v) = %q, want %q", tc.port.Port, got, tc.want)
		}
	}
}

func TestLogUnresolvedNamedPortLogsOnce(t *testing.T) {
	var buf bytes.Buffer
	klog.LogToStderr(false)
	klog.SetOutput(&buf)
	// klog also copies a warning to the info output; count it once.
	klog.SetOutputBySeverity("INFO", io.Discard)
	t.Cleanup(func() { klog.SetOutput(nil); klog.LogToStderr(true) })

	for range 3 {
		logUnresolvedNamedPort("ns/log-once", ingressChain, "http", corev1.ProtocolTCP)
	}
	logUnresolvedNamedPort("ns/log-once", egressChain, "http", corev1.ProtocolTCP)
	klog.Flush()

	if got := strings.Count(buf.String(), `policy ns/log-once: ingress named port "http" (TCP) matches no container port`); got != 1 {
		t.Errorf("ingress warning logged %d times, want 1:\n%s", got, buf.String())
	}
	if got := strings.Count(buf.String(), `policy ns/log-once: egress named port "http"`); got != 1 {
		t.Errorf("egress warning logged %d times, want 1:\n%s", got, buf.String())
	}
}

// TestApplyPolicyPortsRulesIngressNamedPorts checks that ingress named ports
// resolve against the selected pod's container ports and join the numeric
// ports, and that a name matching no container port adds nothing.
func TestApplyPolicyPortsRulesIngressNamedPorts(t *testing.T) {
	c, newNS := nftest.OpenSystemConn(t, true, DEBUG)
	defer nftest.CleanupSystemConn(t, newNS, DEBUG)
	defer c.CloseLasting()
	c.FlushRuleset()
	defer c.FlushRuleset()

	nftState, _, _, _, err := prepareEnv(c, false)
	if err != nil {
		t.Fatalf("failed to prepare test env: %s", err.Error())
	}

	tcp, udp := corev1.ProtocolTCP, corev1.ProtocolUDP
	p80, http, web, dns, missing := intstr.FromInt32(80), intstr.FromString("http"), intstr.FromString("web"), intstr.FromString("dns"), intstr.FromString("missing")
	local := []corev1.ContainerPort{
		{Name: "http", ContainerPort: 8080},
		{Name: "web", ContainerPort: 80}, // same as the numeric port: added once
		{Name: "dns", ContainerPort: 53, Protocol: corev1.ProtocolUDP},
	}
	ports := []multiv1beta1.MultiNetworkPolicyPort{
		{Protocol: &tcp, Port: &p80},
		{Protocol: &tcp, Port: &http},
		{Port: &web},
		{Protocol: &udp, Port: &dns},
		{Protocol: &tcp, Port: &missing},
	}
	if err := nftState.applyPolicyPortsRules(nftState.ingressChain.Name, nftState.ingressChain, "ns/p", ports, 0, namedPortTargets{local: local}); err != nil {
		t.Fatalf("applyPolicyPortsRules() mixed: %v", err)
	}
	// Only unresolvable names: the rule must allow no port at all.
	if err := nftState.applyPolicyPortsRules(nftState.ingressChain.Name, nftState.ingressChain, "ns/p", []multiv1beta1.MultiNetworkPolicyPort{{Port: &missing}}, 1, namedPortTargets{local: local}); err != nil {
		t.Fatalf("applyPolicyPortsRules() unresolved: %v", err)
	}
	// A port entry without a port allows all TCP ports, next to a named
	// port whether that resolves or not.
	for index, named := range []*intstr.IntOrString{&missing, &http} {
		if err := nftState.applyPolicyPortsRules(nftState.ingressChain.Name, nftState.ingressChain, "ns/p", []multiv1beta1.MultiNetworkPolicyPort{{}, {Port: named}}, index+2, namedPortTargets{local: local}); err != nil {
			t.Fatalf("applyPolicyPortsRules() empty entry and %s: %v", named.StrVal, err)
		}
	}
	if err := nftState.nft.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	for _, suffix := range []string{"ports_2_tcp", "ports_3_tcp"} {
		if got := portIntervals(t, c, findSetByComment(t, c, nftState.filter, "", suffix)); !slices.Equal(got, [][2]uint16{{1, 65535}}) {
			t.Errorf("%s = %v, want [[1 65535]]", suffix, got)
		}
	}
	if got := portIntervals(t, c, findSetByComment(t, c, nftState.filter, "", "ports_0_tcp")); !slices.Equal(got, [][2]uint16{{80, 80}, {8080, 8080}}) {
		t.Errorf("tcp ports = %v, want [[80 80] [8080 8080]]", got)
	}
	if got := portIntervals(t, c, findSetByComment(t, c, nftState.filter, "", "ports_0_udp")); !slices.Equal(got, [][2]uint16{{53, 53}}) {
		t.Errorf("udp ports = %v, want [[53 53]]", got)
	}

	rules := portsChainRules(t, c, nftState, ingressChain, 1)
	if len(rules) != 0 {
		t.Fatalf("unresolved ports chain has %d rules, want none: %v", len(rules), ruleComments(rules))
	}
	if slices.ContainsFunc(sets(t, c, nftState.filter), func(s *nftables.Set) bool { return strings.Contains(s.Comment, "ports_1_") }) {
		t.Error("unresolved named port created a port set")
	}
}

// TestApplyPolicyPortsRulesEgressNamedPorts checks that egress named ports
// become (peer address . port) tuples per peer pod, including peers that give
// the name different numbers, next to the numeric ports.
func TestApplyPolicyPortsRulesEgressNamedPorts(t *testing.T) {
	c, newNS := nftest.OpenSystemConn(t, true, DEBUG)
	defer nftest.CleanupSystemConn(t, newNS, DEBUG)
	defer c.CloseLasting()
	c.FlushRuleset()
	defer c.FlushRuleset()

	nftState, _, _, _, err := prepareEnv(c, false)
	if err != nil {
		t.Fatalf("failed to prepare test env: %s", err.Error())
	}

	http8080 := []corev1.ContainerPort{{Name: "http", ContainerPort: 8080}}
	peers := map[string]*controllers.PodInfo{
		"10.1.1.1": {ContainerPorts: http8080},
		"10.1.1.2": {ContainerPorts: []corev1.ContainerPort{{Name: "http", ContainerPort: 9090}}},
		"fd00::5":  {ContainerPorts: http8080},
		"10.1.1.3": {ContainerPorts: []corev1.ContainerPort{{Name: "other", ContainerPort: 8080}}},
		"10.1.1.4": {ContainerPorts: []corev1.ContainerPort{{Name: "http", ContainerPort: 8080, Protocol: corev1.ProtocolUDP}}},
	}
	tcp := corev1.ProtocolTCP
	p80, http, missing := intstr.FromInt32(80), intstr.FromString("http"), intstr.FromString("missing")
	ports := []multiv1beta1.MultiNetworkPolicyPort{{Protocol: &tcp, Port: &p80}, {Protocol: &tcp, Port: &http}}

	if err := nftState.applyPolicyPortsRules(nftState.egressChain.Name, nftState.egressChain, "ns/p", ports, 0, namedPortTargets{peers: peers}); err != nil {
		t.Fatalf("applyPolicyPortsRules() mixed: %v", err)
	}
	if err := nftState.applyPolicyPortsRules(nftState.egressChain.Name, nftState.egressChain, "ns/p", []multiv1beta1.MultiNetworkPolicyPort{{Port: &missing}}, 1, namedPortTargets{peers: peers}); err != nil {
		t.Fatalf("applyPolicyPortsRules() unresolved: %v", err)
	}
	if err := nftState.nft.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	if got := portIntervals(t, c, findSetByComment(t, c, nftState.filter, "", "ports_0_tcp")); !slices.Equal(got, [][2]uint16{{80, 80}}) {
		t.Errorf("tcp ports = %v, want [[80 80]]", got)
	}
	if got, want := tupleElements(t, c, findSetByComment(t, c, nftState.filter, "", "ports_0_tcp_named4")), []string{"10.1.1.1:8080", "10.1.1.2:9090"}; !slices.Equal(got, want) {
		t.Errorf("tcp IPv4 tuples = %v, want %v", got, want)
	}
	if got, want := tupleElements(t, c, findSetByComment(t, c, nftState.filter, "", "ports_0_tcp_named6")), []string{"[fd00::5]:8080"}; !slices.Equal(got, want) {
		t.Errorf("tcp IPv6 tuples = %v, want %v", got, want)
	}
	if slices.ContainsFunc(sets(t, c, nftState.filter), func(s *nftables.Set) bool { return strings.Contains(s.Comment, "udp") }) {
		t.Error("a TCP named port created a UDP set")
	}

	rules := portsChainRules(t, c, nftState, egressChain, 0)
	if n := countRules(rules, "named-port-set:"); n != 2 {
		t.Errorf("ports chain has %d named port rules, want 2: %v", n, ruleComments(rules))
	}
	if rules := portsChainRules(t, c, nftState, egressChain, 1); len(rules) != 0 {
		t.Errorf("unresolved ports chain has %d rules, want none: %v", len(rules), ruleComments(rules))
	}
}

// TestEgressNamedPortTuplesFollowPeers checks that a reconcile after a peer
// change rewrites the tuples: a changed port replaces the old element, and the
// set and rule of a family without peers left are removed by the cleanup.
func TestEgressNamedPortTuplesFollowPeers(t *testing.T) {
	c, newNS := nftest.OpenSystemConn(t, true, DEBUG)
	defer nftest.CleanupSystemConn(t, newNS, DEBUG)
	defer c.CloseLasting()
	c.FlushRuleset()
	defer c.FlushRuleset()

	nftState, _, _, _, err := prepareEnv(c, false)
	if err != nil {
		t.Fatalf("failed to prepare test env: %s", err.Error())
	}

	http := intstr.FromString("http")
	ports := []multiv1beta1.MultiNetworkPolicyPort{{Port: &http}}
	reconcile := func(peers map[string]*controllers.PodInfo) {
		t.Helper()
		if err := nftState.applyPolicyPortsRules(nftState.egressChain.Name, nftState.egressChain, "ns/p", ports, 0, namedPortTargets{peers: peers}); err != nil {
			t.Fatalf("applyPolicyPortsRules(): %v", err)
		}
		if err := nftState.nft.Flush(); err != nil {
			t.Fatalf("flush: %v", err)
		}
		if err := nftState.cleanup(); err != nil {
			t.Fatalf("cleanup(): %v", err)
		}
	}

	reconcile(map[string]*controllers.PodInfo{
		"10.1.1.1": {ContainerPorts: []corev1.ContainerPort{{Name: "http", ContainerPort: 8080}}},
		"fd00::5":  {ContainerPorts: []corev1.ContainerPort{{Name: "http", ContainerPort: 8080}}},
	})
	reconcile(map[string]*controllers.PodInfo{
		"10.1.1.1": {ContainerPorts: []corev1.ContainerPort{{Name: "http", ContainerPort: 9090}}},
		"10.1.1.2": {ContainerPorts: []corev1.ContainerPort{{Name: "http", ContainerPort: 8080}}},
	})

	if got, want := tupleElements(t, c, findSetByComment(t, c, nftState.filter, "", "ports_0_tcp_named4")), []string{"10.1.1.1:9090", "10.1.1.2:8080"}; !slices.Equal(got, want) {
		t.Errorf("tcp IPv4 tuples = %v, want %v", got, want)
	}
	if slices.ContainsFunc(sets(t, c, nftState.filter), func(s *nftables.Set) bool { return strings.HasSuffix(s.Comment, "_named6") }) {
		t.Error("the IPv6 tuple set is left after its last peer went away")
	}
	if n := countRules(portsChainRules(t, c, nftState, egressChain, 0), "named-port-set:"); n > 1 {
		t.Errorf("ports chain has %d named port rules, want at most the IPv4 one", n)
	}
}

// TestEgressNamedPortTupleRulesSurviveCleanup checks that the tuple rules
// match their kernel copies, so that the cleanup keeps them and a second apply
// does not add them again. The kernel dumps the IPv6 rule's port register,
// NFT_REG32_04, as NFT_REG_2.
func TestEgressNamedPortTupleRulesSurviveCleanup(t *testing.T) {
	c, newNS := nftest.OpenSystemConn(t, true, DEBUG)
	defer nftest.CleanupSystemConn(t, newNS, DEBUG)
	defer c.CloseLasting()
	c.FlushRuleset()
	defer c.FlushRuleset()

	nftState, _, _, _, err := prepareEnv(c, false)
	if err != nil {
		t.Fatalf("failed to prepare test env: %s", err.Error())
	}

	http := intstr.FromString("http")
	http8080 := []corev1.ContainerPort{{Name: "http", ContainerPort: 8080}}
	peers := map[string]*controllers.PodInfo{"10.1.1.1": {ContainerPorts: http8080}, "fd00::5": {ContainerPorts: http8080}}
	for round := 1; round <= 2; round++ {
		if err := nftState.applyPolicyPortsRules(nftState.egressChain.Name, nftState.egressChain, "ns/p", []multiv1beta1.MultiNetworkPolicyPort{{Port: &http}}, 0, namedPortTargets{peers: peers}); err != nil {
			t.Fatalf("round %d: applyPolicyPortsRules(): %v", round, err)
		}
		if err := nftState.nft.Flush(); err != nil {
			t.Fatalf("round %d: flush: %v", round, err)
		}
		if err := nftState.cleanup(); err != nil {
			t.Fatalf("round %d: cleanup(): %v", round, err)
		}
		rules := portsChainRules(t, c, nftState, egressChain, 0)
		for _, family := range []string{"_named4", "_named6"} {
			if n := countRules(rules, family); n != 1 {
				t.Errorf("round %d: ports chain has %d %s rules, want 1: %v", round, n, family, ruleComments(rules))
			}
		}
	}
}

// TestApplyPodRulesNamedPorts runs a policy with named ports through
// applyPodRules: ingress resolves on the selected pod, egress on each selector
// peer, whose container ports differ.
func TestApplyPodRulesNamedPorts(t *testing.T) {
	c, newNS := nftest.OpenSystemConn(t, true, DEBUG)
	defer nftest.CleanupSystemConn(t, newNS, DEBUG)
	defer c.CloseLasting()
	c.FlushRuleset()
	defer c.FlushRuleset()

	nftState, testNs, deps, podMockInfo, err := prepareEnv(c, true)
	if err != nil {
		t.Fatalf("failed to prepare test env: %s", err.Error())
	}

	policyNetAttach := fmt.Sprintf("%s/policy-net-1", testNs)
	podMockInfo.Interfaces = append(podMockInfo.Interfaces, controllers.InterfaceInfo{
		NetattachName: policyNetAttach, InterfaceType: "macvlan", InterfaceName: "net1", IPs: []string{"10.1.1.9"},
	})
	podMockInfo.ContainerPorts = []corev1.ContainerPort{{Name: "http", ContainerPort: 7070}}
	setContainerPorts(deps, testNs, "testpod1", corev1.ContainerPort{Name: "http", ContainerPort: 8080})
	setContainerPorts(deps, testNs, "testpod2", corev1.ContainerPort{Name: "http", ContainerPort: 9090})

	http := intstr.FromString("http")
	ports := []multiv1beta1.MultiNetworkPolicyPort{{Port: &http}}
	peers := []multiv1beta1.MultiNetworkPolicyPeer{{PodSelector: &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "app", Operator: metav1.LabelSelectorOpIn, Values: []string{"test", "test2"}}},
	}}}
	policy := &multiv1beta1.MultiNetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "named", Namespace: testNs},
		Spec: multiv1beta1.MultiNetworkPolicySpec{
			Ingress:     []multiv1beta1.MultiNetworkPolicyIngressRule{{Ports: ports, From: peers}},
			Egress:      []multiv1beta1.MultiNetworkPolicyEgressRule{{Ports: ports, To: peers}},
			PolicyTypes: []multiv1beta1.MultiPolicyType{multiv1beta1.PolicyTypeIngress, multiv1beta1.PolicyTypeEgress},
		},
	}
	policyNetworks := []string{policyNetAttach}
	for _, chain := range []*nftables.Chain{nftState.ingressChain, nftState.egressChain} {
		if _, err := nftState.applyPodRules(context.Background(), deps, deps.cfg, chain, podMockInfo, policy, policyNetworks); err != nil {
			t.Fatalf("applyPodRules(%s) failed: %v", chain.Name, err)
		}
	}
	if err := nftState.nft.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	if got := portIntervals(t, c, findSetByComment(t, c, nftState.filter, "multi_ingress", "ports_0_tcp")); !slices.Equal(got, [][2]uint16{{7070, 7070}}) {
		t.Errorf("ingress tcp ports = %v, want [[7070 7070]]", got)
	}
	if got, want := tupleElements(t, c, findSetByComment(t, c, nftState.filter, "multi_egress", "ports_0_tcp_named4")), []string{"10.1.1.1:8080", "10.1.1.2:9090"}; !slices.Equal(got, want) {
		t.Errorf("egress tcp tuples = %v, want %v", got, want)
	}
}

// TestEgressNamedPortPeers checks which pods egress named ports resolve
// against: none without named ports, the selector peers, and without peers all
// pods except the selected pod itself. A podSelector-only peer matches pods in
// the policy namespace only, like the peer IP sets.
func TestEgressNamedPortPeers(t *testing.T) {
	deps := newTestPolicyDeps()
	addNamespace(deps, "ns")
	deps.netdefMap[types.NamespacedName{Namespace: "ns", Name: "net"}] = "multi"
	for name, ip := range map[string]string{"self": "10.2.0.1", "a": "10.2.0.2", "b": "10.2.0.3"} {
		addPod(deps, NewFakePodWithNetAnnotation("ns", name, "net", NewFakeNetworkStatus("ns", "net", "192.168.2.1", ip), map[string]string{"name": name}))
	}
	// Same label as "a", on the same network, in another namespace.
	addNamespace(deps, "other")
	addPod(deps, NewFakePodWithNetAnnotation("other", "a", "ns/net", NewFakeNetworkStatus("ns", "net", "192.168.2.1", "10.2.0.4"), map[string]string{"name": "a"}))
	self := &controllers.PodInfo{Interfaces: []controllers.InterfaceInfo{{NetattachName: "ns/net", IPs: []string{"10.2.0.1"}}}}
	policyNetworks := []string{"ns/net"}
	http, p80 := intstr.FromString("http"), intstr.FromInt32(80)
	onlyA := []multiv1beta1.MultiNetworkPolicyPeer{{PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"name": "a"}}}}
	otherA := []multiv1beta1.MultiNetworkPolicyPeer{{
		NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"nsname": "other"}},
		PodSelector:       &metav1.LabelSelector{MatchLabels: map[string]string{"name": "a"}},
	}}
	ipBlock := []multiv1beta1.MultiNetworkPolicyPeer{{IPBlock: &multiv1beta1.IPBlock{CIDR: "10.2.0.0/24"}}}
	// The IPBlock takes precedence over the selector, so the selected pods
	// must not add tuples outside the CIDR.
	ipBlockAndSelector := []multiv1beta1.MultiNetworkPolicyPeer{{
		IPBlock:     &multiv1beta1.IPBlock{CIDR: "10.9.0.0/24"},
		PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"name": "a"}},
	}}

	cases := []struct {
		name string
		rule multiv1beta1.MultiNetworkPolicyEgressRule
		want []string
	}{
		{"numeric ports only", multiv1beta1.MultiNetworkPolicyEgressRule{Ports: []multiv1beta1.MultiNetworkPolicyPort{{Port: &p80}}}, nil},
		{"selector peer", multiv1beta1.MultiNetworkPolicyEgressRule{Ports: []multiv1beta1.MultiNetworkPolicyPort{{Port: &http}}, To: onlyA}, []string{"10.2.0.2"}},
		{"namespace and pod selector peer", multiv1beta1.MultiNetworkPolicyEgressRule{Ports: []multiv1beta1.MultiNetworkPolicyPort{{Port: &http}}, To: otherA}, []string{"10.2.0.4"}},
		{"ipBlock peer", multiv1beta1.MultiNetworkPolicyEgressRule{Ports: []multiv1beta1.MultiNetworkPolicyPort{{Port: &http}}, To: ipBlock}, nil},
		{"ipBlock and selector peer", multiv1beta1.MultiNetworkPolicyEgressRule{Ports: []multiv1beta1.MultiNetworkPolicyPort{{Port: &http}}, To: ipBlockAndSelector}, nil},
		{"no peers", multiv1beta1.MultiNetworkPolicyEgressRule{Ports: []multiv1beta1.MultiNetworkPolicyPort{{Port: &http}}}, []string{"10.2.0.2", "10.2.0.3", "10.2.0.4"}},
	}
	for _, tc := range cases {
		peers, err := egressNamedPortPeers(context.Background(), deps, tc.rule, "ns", self, policyNetworks)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		got := slices.Sorted(maps.Keys(peers))
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: peers = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func setContainerPorts(deps *testPolicyDeps, namespace, name string, ports ...corev1.ContainerPort) {
	key := types.NamespacedName{Namespace: namespace, Name: name}
	info := deps.podMap[key]
	info.ContainerPorts = ports
	deps.podMap[key] = info
}

// tupleElements returns the "addr:port" elements of a named port tuple set.
func tupleElements(t *testing.T, c *nftables.Conn, set *nftables.Set) []string {
	t.Helper()
	els, err := c.GetSetElements(set)
	if err != nil {
		t.Fatalf("GetSetElements(%s): %v", set.Name, err)
	}
	var out []string
	for _, e := range els {
		addrLen := len(e.Key) - 4
		addr, ok := netip.AddrFromSlice(e.Key[:addrLen])
		if !ok || (addrLen != 4 && addrLen != 16) {
			t.Fatalf("set %s: bad key %x", set.Name, e.Key)
		}
		out = append(out, netip.AddrPortFrom(addr, binaryutil.BigEndian.Uint16(e.Key[addrLen:])).String())
	}
	slices.Sort(out)
	return out
}

func portsChainRules(t *testing.T, c *nftables.Conn, n *nftState, direction string, index int) []*nftables.Rule {
	t.Helper()
	name := nftNameWithSuffix(direction, "-", fmt.Sprintf("%s-%d", portsChainSuffix, index))
	rules, err := c.GetRules(n.filter, &nftables.Chain{Name: name, Table: n.filter})
	if err != nil {
		t.Fatalf("GetRules(%s): %v", name, err)
	}
	return rules
}

func countRules(rules []*nftables.Rule, commentPart string) int {
	return len(slices.DeleteFunc(ruleComments(rules), func(s string) bool { return !strings.Contains(s, commentPart) }))
}

func ruleComments(rules []*nftables.Rule) []string {
	out := make([]string, 0, len(rules))
	for _, r := range rules {
		out = append(out, string(r.UserData))
	}
	return out
}
