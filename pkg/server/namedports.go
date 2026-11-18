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
	"cmp"
	"context"
	"fmt"
	"math"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"

	nftables "github.com/google/nftables"
	"github.com/google/nftables/binaryutil"
	"github.com/google/nftables/expr"
	multiv1beta1 "github.com/k8snetworkplumbingwg/multi-networkpolicy/pkg/apis/k8s.cni.cncf.io/v1beta1"
	"github.com/telekom/multi-networkpolicy-nftables/pkg/controllers"
	"golang.org/x/sys/unix"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/klog/v2"
)

// Named ports resolve like in Kubernetes NetworkPolicy: against the container
// ports of the pod that receives the traffic. For ingress, that is the
// selected pod, so a named port becomes a numeric port in the port set. For
// egress, it is each peer pod, and peers can give a name different numbers, so
// a named port becomes (peer address . port) tuples in a concatenated set.

// Registers for a concatenated (address . port) lookup key. The address starts
// at NFT_REG_1 (= NFT_REG32_00); the port follows in the next 32-bit register.
const (
	regTupleAddr   = 1  // NFT_REG_1
	regTuplePortV4 = 9  // NFT_REG32_01, after a 4-byte IPv4 address
	regTuplePortV6 = 12 // NFT_REG32_04, after a 16-byte IPv6 address
)

// namedPortTargets holds what the named ports of a rule resolve against: the
// container ports of the selected pod for ingress, and the peer pods by IP for
// egress.
type namedPortTargets struct {
	local []v1.ContainerPort
	peers map[string]*controllers.PodInfo
}

// namedPortTupleKey identifies one concatenated set of egress named-port tuples.
type namedPortTupleKey struct {
	proto v1.Protocol
	v6    bool
}

func compareNamedPortTupleKeys(a, b namedPortTupleKey) int {
	if c := cmp.Compare(a.proto, b.proto); c != 0 {
		return c
	}
	if a.v6 == b.v6 {
		return 0
	}
	if a.v6 {
		return 1
	}
	return -1
}

// namedPortName returns the name of a named port, or "" for a numeric or
// absent port. A numeric string such as "8080" is a numeric port.
func namedPortName(port multiv1beta1.MultiNetworkPolicyPort) string {
	if port.Port == nil || port.Port.Type != intstr.String {
		return ""
	}
	if _, err := strconv.Atoi(port.Port.StrVal); err == nil {
		return ""
	}
	return port.Port.StrVal
}

// portProtocol returns the protocol of a policy port; it defaults to TCP.
func portProtocol(port multiv1beta1.MultiNetworkPolicyPort) v1.Protocol {
	if port.Protocol != nil {
		return *port.Protocol
	}
	return v1.ProtocolTCP
}

// resolveNamedPort returns the number of the container port with the given
// name and protocol.
func resolveNamedPort(ports []v1.ContainerPort, name string, proto v1.Protocol) (uint16, bool) {
	for _, p := range ports {
		cproto := p.Protocol
		if cproto == "" {
			cproto = v1.ProtocolTCP
		}
		if p.Name == name && cproto == proto && p.ContainerPort >= 1 && p.ContainerPort <= math.MaxUint16 {
			return uint16(p.ContainerPort), true //nolint:gosec // G115: checked in range [1, 65535] above
		}
	}
	return 0, false
}

// resolveRuleNamedPorts splits the named ports off the ports of a rule. For
// ingress, it returns them resolved to numeric ports, next to the numeric
// ports. For egress, it returns them as (peer address . port) tuples. It also
// reports whether the rule had named ports. A named port that resolves to
// nothing is logged and dropped, so it allows nothing.
func resolveRuleNamedPorts(policyName, chainName string, ports []multiv1beta1.MultiNetworkPolicyPort, targets namedPortTargets) (
	[]multiv1beta1.MultiNetworkPolicyPort, map[namedPortTupleKey]map[string]nftables.SetElement, bool) {
	numeric := make([]multiv1beta1.MultiNetworkPolicyPort, 0, len(ports))
	tuples := make(map[namedPortTupleKey]map[string]nftables.SetElement)
	hasNamed := false
	for _, port := range ports {
		name := namedPortName(port)
		if name == "" {
			numeric = append(numeric, port)
			continue
		}
		hasNamed = true
		proto := portProtocol(port)
		if !isIngressChain(chainName) {
			if !addNamedPortTuples(tuples, targets.peers, name, proto) {
				logUnresolvedNamedPort(policyName, chainName, name, proto)
			}
			continue
		}
		num, ok := resolveNamedPort(targets.local, name, proto)
		if !ok {
			logUnresolvedNamedPort(policyName, chainName, name, proto)
			continue
		}
		numeric = append(numeric, multiv1beta1.MultiNetworkPolicyPort{
			Protocol: &proto,
			Port:     &intstr.IntOrString{Type: intstr.Int, IntVal: int32(num)},
		})
	}
	return numeric, tuples, hasNamed
}

// addNamedPortTuples adds a (peer address . port) element for every peer pod
// on which name resolves, and reports whether it resolved on any.
func addNamedPortTuples(tuples map[namedPortTupleKey]map[string]nftables.SetElement, peers map[string]*controllers.PodInfo, name string, proto v1.Protocol) bool {
	found := false
	for ip, peer := range peers {
		num, ok := resolveNamedPort(peer.ContainerPorts, name, proto)
		if !ok {
			continue
		}
		addr, err := netip.ParseAddr(ip)
		if err != nil {
			klog.Errorf("cannot parse peer address %q: %v", ip, err)
			continue
		}
		addr = addr.Unmap()
		// inet_service is 2 bytes, padded to the 4-byte register.
		elemKey := append(addr.AsSlice(), binaryutil.BigEndian.PutUint16(num)...)
		elemKey = append(elemKey, 0, 0)
		key := namedPortTupleKey{proto: proto, v6: addr.Is6()}
		if tuples[key] == nil {
			tuples[key] = make(map[string]nftables.SetElement)
		}
		tuples[key][string(elemKey)] = nftables.SetElement{Key: elemKey}
		found = true
	}
	return found
}

// egressNamedPortPeers returns the peer pods, by IP, that the named ports of
// an egress rule resolve against, or nil if the rule has no named ports. A rule
// without peers allows all destinations, so its named ports resolve against
// all pods. IPBlock peers have no container ports and are left out.
func egressNamedPortPeers(ctx context.Context, deps controllers.PolicyDeps, rule multiv1beta1.MultiNetworkPolicyEgressRule, policyNamespace string,
	podInfo *controllers.PodInfo, policyNetworks []string) (map[string]*controllers.PodInfo, error) {
	if !slices.ContainsFunc(rule.Ports, func(p multiv1beta1.MultiNetworkPolicyPort) bool { return namedPortName(p) != "" }) {
		return nil, nil
	}
	peers := rule.To
	if len(peers) == 0 {
		peers = []multiv1beta1.MultiNetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{}}}
	}
	all := make(map[string]*controllers.PodInfo)
	for _, peer := range peers {
		// An IPBlock takes precedence over selectors in the same peer, as in
		// applyPolicyPeersRules; its pods must not widen the CIDR.
		if peer.IPBlock != nil || (peer.PodSelector == nil && peer.NamespaceSelector == nil) {
			continue
		}
		pods, err := selectorPeerPods(ctx, deps, peer, policyNamespace, podInfo, policyNetworks)
		if err != nil {
			return nil, err
		}
		for ip, pod := range pods {
			all[ip] = pod
		}
	}
	return all, nil
}

// unresolvedNamedPorts records which unresolvable named ports were logged.
//
// ponytail: keys of deleted policies are never removed; bounded by the
// distinct policy/port names seen. Prune on policy delete if that ever grows.
var unresolvedNamedPorts sync.Map

// logUnresolvedNamedPort logs, once per policy, direction, name and protocol,
// a named port that matches no container port and so allows nothing.
func logUnresolvedNamedPort(policyName, chainName, name string, proto v1.Protocol) {
	direction := "egress"
	if isIngressChain(chainName) {
		direction = "ingress"
	}
	key := strings.Join([]string{policyName, direction, name, string(proto)}, "|")
	if _, logged := unresolvedNamedPorts.LoadOrStore(key, struct{}{}); !logged {
		klog.Warningf("policy %s: %s named port %q (%s) matches no container port and allows nothing", policyName, direction, name, proto)
	}
}

// applyNamedPortTupleRule sets both the port and the peer mark for packets
// whose destination address and port are in the tuple set of key.
func (n *nftState) applyNamedPortTupleRule(portsName string, chain *nftables.Chain, policyName string, key namedPortTupleKey, elements []nftables.SetElement) error {
	suffix, unixProto := getProtocolInfo(key.proto)
	addrType, family := nftables.TypeIPAddr, "4"
	nfProto, addrOffset, addrLen, portReg := uint8(unix.NFPROTO_IPV4), IPv4OffSet+net.IPv4len, uint32(net.IPv4len), uint32(regTuplePortV4)
	if key.v6 {
		addrType, family = nftables.TypeIP6Addr, "6"
		nfProto, addrOffset, addrLen, portReg = uint8(unix.NFPROTO_IPV6), IPv6OffSet+net.IPv6len, uint32(net.IPv6len), uint32(regTuplePortV6)
	}

	setName := nftNameWithSuffix(getSetName(portsName), "_", suffix+"_named"+family)
	set := &nftables.Set{
		Table:   chain.Table,
		Name:    setName,
		Comment: setName,
		KeyType: nftables.MustConcatSetType(addrType, nftables.TypeInetService),
	}
	if err := n.updateSet(set, elements); err != nil {
		return fmt.Errorf("failed to update named port set %q: %w", setName, err)
	}

	_, err := n.updateRule(&nftables.Rule{
		Table:    chain.Table,
		Chain:    chain,
		UserData: userDataComment(fmt.Sprintf("policy:%s, name:%s, named-port-set:%s", policyName, portsName, set.Name)),
		Exprs: []expr.Any{
			&expr.Meta{Key: expr.MetaKeyNFPROTO, Register: 0x1},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 0x1, Data: []byte{nfProto}},
			&expr.Meta{Key: expr.MetaKeyL4PROTO, Register: 0x1},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 0x1, Data: unixProto},
			&expr.Payload{DestRegister: regTupleAddr, Base: expr.PayloadBaseNetworkHeader, Offset: addrOffset, Len: addrLen},
			&expr.Payload{DestRegister: portReg, Base: expr.PayloadBaseTransportHeader, Offset: 2, Len: 2},
			&expr.Lookup{SourceRegister: regTupleAddr, SetName: set.Name, SetID: set.ID},
			&expr.Counter{},
			// The tuple holds the peer address, so it matches both the port
			// and the peer of the rule.
			&expr.Meta{Key: expr.MetaKeyMARK, Register: 0x1},
			&expr.Bitwise{
				SourceRegister: 0x1,
				DestRegister:   0x1,
				Len:            4,
				Mask:           binaryutil.NativeEndian.PutUint32(^matchRuleMark),
				Xor:            binaryutil.NativeEndian.PutUint32(matchRuleMark),
			},
			&expr.Meta{Key: expr.MetaKeyMARK, SourceRegister: true, Register: 0x1},
		},
	}, n.nft.AddRule, false)
	return err
}
