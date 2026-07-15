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
	"fmt"
	"hash/fnv"
	"net"
	"slices"
	"strconv"
	"strings"

	tc "github.com/florianl/go-tc"
	"github.com/florianl/go-tc/core"
	multiv1beta1 "github.com/k8snetworkplumbingwg/multi-networkpolicy/pkg/apis/k8s.cni.cncf.io/v1beta1"
	"github.com/telekom/multi-networkpolicy-nftables/pkg/controllers"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// L2/L3/L4 protocol constants (IANA / EtherType). Hardcoded so the pure
// translation layer does not need golang.org/x/sys/unix.
const (
	ethTypeIPv4 uint16 = 0x0800

	ipProtoTCP  uint8 = 6
	ipProtoUDP  uint8 = 17
	ipProtoSCTP uint8 = 132
)

// Direction is the MultiNetworkPolicy rule direction that a FlowerRule
// enforces. It is mapped to a VF-representor tc qdisc parent in toObject.
//
// DIRECTION MAPPING (IMPORTANT — needs empirical hardware validation):
// From the host's point of view the VF representor is a mirror of the pod's
// VF, so traffic directions are INVERTED relative to the pod:
//
//	representor INGRESS qdisc (packets the representor receives) == traffic
//	    leaving the pod           == MultiNetworkPolicy EGRESS  (DirEgress)
//	representor EGRESS qdisc  (packets the representor transmits) == traffic
//	    entering the pod          == MultiNetworkPolicy INGRESS (DirIngress)
//
// This inversion is the crux of the tc flower backend and MUST be validated on
// real switchdev hardware before this backend is trusted in production.
type Direction int

const (
	// DirIngress enforces MultiNetworkPolicy ingress (traffic TO the pod). It
	// is installed on the representor's EGRESS parent.
	DirIngress Direction = iota
	// DirEgress enforces MultiNetworkPolicy egress (traffic FROM the pod). It
	// is installed on the representor's INGRESS parent.
	DirEgress
)

func (d Direction) String() string {
	if d == DirIngress {
		return "ingress"
	}
	return "egress"
}

// Verdict is the terminal action of a FlowerRule.
type Verdict int

const (
	// VerdictAccept passes the packet (tc gact TC_ACT_OK).
	VerdictAccept Verdict = iota
	// VerdictDrop drops the packet (tc gact TC_ACT_SHOT).
	VerdictDrop
)

func (v Verdict) String() string {
	if v == VerdictDrop {
		return "drop"
	}
	return "accept"
}

// tc gact control actions (from include/uapi/linux/pkt_cls.h).
const (
	tcActOK        uint32 = 0          // TC_ACT_OK   — accept/pass
	tcActPipe      uint32 = 3          // TC_ACT_PIPE — continue to the next action in the list
	tcActShot      uint32 = 2          // TC_ACT_SHOT — drop
	tcActGotoChain uint32 = 0x40000000 // TC_ACT_GOTO_CHAIN — jump to (control & TC_ACT_EXT_VAL_MASK) chain
)

// conntrack ct_state flag bits (from include/uapi/linux/pkt_cls.h,
// TCA_FLOWER_KEY_CT_FLAGS_*). These are matched via the flower KeyCtState /
// KeyCtStateMask key pair. A match encodes (value, mask): a bit set in the mask
// is examined; the corresponding value bit is the required setting.
const (
	ctStateNew         uint16 = 1 << 0 // +new  — first packet of a new connection
	ctStateEstablished uint16 = 1 << 1 // +est  — part of an established connection
	ctStateRelated     uint16 = 1 << 2 // +rel  — related to an established connection
	ctStateTracked     uint16 = 1 << 3 // +trk  — the packet has been through conntrack
	ctStateInvalid     uint16 = 1 << 4 // +inv  — conntrack could not classify the packet
	ctStateReply       uint16 = 1 << 5 // +rpl  — reply direction of a tracked connection
)

// conntrack ct action bitfield (struct tc_ct / TCA_CT_ACT_* from
// include/uapi/linux/tc_act/tc_ct.h). Carried in go-tc's Ct.Action.
const (
	ctActCommit uint16 = 1 // TCA_CT_ACT_COMMIT — commit the (new) connection to conntrack
	ctActForce  uint16 = 2 // TCA_CT_ACT_FORCE
	ctActClear  uint16 = 4 // TCA_CT_ACT_CLEAR
	ctActNAT    uint16 = 8 // TCA_CT_ACT_NAT
)

// The full ct_state and ct-action bit sets are declared above to document the
// UAPI as a unit; only a subset is currently emitted. Reference the remainder
// so the enum stays complete without tripping the unused-symbol linter.
var _ = [...]uint16{ctStateNew, ctStateReply, ctActForce, ctActClear, ctActNAT}

// CT pipeline chain numbers. In stateful (CT offload) mode traffic is split
// across two tc filter chains per representor parent:
//   - chain 0 (ctEntryChain) is the entry chain: it dispatches untracked
//     packets through conntrack and statefully accepts established/related
//     return traffic (mirroring the nft backend's allowConntracked).
//   - chain 1 (ctPolicyChain) holds the per-policy allow/except/default-deny
//     rules — the same first-match set as the stateless pipeline, except each
//     accept also commits the NEW connection so its reply is tracked.
//
// In stateless mode everything lives in chain 0 (ctEntryChain), exactly as in
// Phase 2, so the stateless golden output is unchanged.
const (
	ctEntryChain  uint32 = 0
	ctPolicyChain uint32 = 1
)

// FlowerRule is an abstract, comparable description of one desired tc flower
// filter. It is intentionally independent of go-tc marshalling so it can be
// unit-tested as golden data. toObject converts it to a go-tc Object.
//
// Only IPv4 is represented in this phase; IPv6 peers/CIDRs are skipped by
// BuildFlowerRules (Phase 3 will add v6).
type FlowerRule struct {
	// Rep is the host VF representor netdev name (the enforcement point).
	Rep string
	// Direction selects the policy direction (and thus representor parent).
	Direction Direction
	// Priority is the tc filter priority. Lower = evaluated first. Assigned
	// deterministically by BuildFlowerRules (see assignPriorities).
	Priority uint16

	// Proto is the IP protocol to match (ipProtoTCP/UDP/SCTP). 0 = match any
	// L4 protocol (no ip_proto key).
	Proto uint8

	// SrcCIDR / DstCIDR carry the address match in CIDR form. For policy
	// ingress the peer is the SOURCE (SrcCIDR); for policy egress the peer is
	// the DESTINATION (DstCIDR). "" means no address match (match-any).
	SrcCIDR string
	DstCIDR string

	// HasPort reports whether an L4 destination-port match is present. When
	// true PortMin..PortMax (inclusive) is the matched range; PortMin==PortMax
	// denotes a single port.
	HasPort bool
	PortMin uint16
	PortMax uint16

	// Verdict is the terminal action.
	Verdict Verdict

	// --- CT (stateful) offload fields (Phase 4) ---
	//
	// These are all zero-valued for stateless (Phase 2) rules, so a stateless
	// FlowerRule is byte-for-byte identical to before and the existing golden
	// output is unchanged. They are only populated when CT is enabled.

	// Chain is the tc filter chain this rule lives in (ctEntryChain /
	// ctPolicyChain). Zero == chain 0 (the default), which is also the only
	// chain used in stateless mode.
	Chain uint32

	// HasCTState reports whether a ct_state match (CTState/CTStateMask) is
	// present on this filter. Used by the chain-0 dispatch/accept rules.
	HasCTState  bool
	CTState     uint16 // ct_state value bits (see ctState* consts)
	CTStateMask uint16 // ct_state mask bits: which flags are examined

	// CTCommit, when set on an accept rule, prepends a `ct commit` action
	// (zone CTZone) before the terminal gact pass, so the NEW connection is
	// committed to conntrack and its reply direction is tracked.
	CTCommit bool

	// CTDispatch, when set, marks the chain-0 untracked-packet rule whose
	// action list is [ct (zone CTZone) pipe, gact goto_chain GotoChain]: send
	// the packet through conntrack, then continue policy evaluation in
	// GotoChain.
	CTDispatch bool
	GotoChain  uint32 // goto-chain target for a CTDispatch rule

	// CTZone is the conntrack zone used by the ct/ct-commit actions. It is
	// per-representor+direction so connections on different reps/directions do
	// not collide in a shared conntrack table.
	CTZone uint16
}

// priority tiers. Lower tier => numerically lower priority => evaluated first.
// Ordering guarantees that a more-specific "except" drop precedes the broader
// "allow" it carves out, and that the catch-all default-deny is evaluated last.
const (
	tierExcept  = 0 // ipBlock.Except drops
	tierAllow   = 1 // allow (accept) rules
	tierDefault = 2 // default-deny catch-all
)

// priority tiers WITHIN the CT entry chain (chain 0). Priorities are numbered
// independently per (direction, chain), so these reuse the same numeric space
// as the policy-chain tiers above without colliding. The ct_state matches are
// mutually exclusive (they differ in the +trk / +est / +rel / +inv bits), so
// ordering only needs to be deterministic, not semantically layered; the tiers
// give a stable, readable order.
const (
	tierCTInvalid  = 0 // +trk+inv  -> drop (defensive)
	tierCTEstRel   = 1 // +trk+est / +trk+rel -> accept (stateful return traffic)
	tierCTDispatch = 2 // -trk      -> ct + goto policy chain
)

// candidate is an in-progress FlowerRule plus the metadata used to allocate a
// deterministic priority.
type candidate struct {
	rule     FlowerRule
	tier     int
	policyID string // "" for the default-deny catch-all
}

// BuildFlowerRules performs the pure translation of the MultiNetworkPolicies
// that apply to pod/iface into an ordered set of abstract FlowerRules. It does
// not touch netlink; toObject / the Driver handle installation.
//
// Semantics mirror pkg/server/netfilterrules.go, re-expressed for a stateless,
// first-match tc flower pipeline:
//   - policy selection: identical to pkg/server (see select.go).
//   - each ingress/egress rule is expanded to the (ports × peers) cross
//     product; every emitted accept filter carries the FULL match (proto +
//     dst port + peer address) because tc flower cannot AND separate filters.
//   - ipBlock: CIDR => accept; each Except CIDR => higher-priority drop.
//   - podSelector/namespaceSelector: resolved to peer pod IPv4 /32 addresses
//     (restricted to the policy networks), one rule per address.
//   - empty peers => match-any address; empty ports => match-any L4.
//   - a lowest-priority catch-all drop is emitted per direction that has at
//     least one selected policy (default-deny).
//
// IPv4 ONLY: IPv6 peer IPs and v6 CIDRs are skipped (TODO Phase 3).
func BuildFlowerRules(ctx context.Context, deps controllers.PolicyDeps, cfg controllers.CommonRuleConfig,
	policyMap controllers.PolicyMap, pod *corev1.Pod, podInfo *controllers.PodInfo, iface controllers.InterfaceInfo) ([]FlowerRule, error) {
	// cfg.CTEnabled selects the stateful CT-offload pipeline; the remaining
	// cfg fields (common-prefix / ICMP rules) are Phase 3+ and kept in the
	// signature for parity with the nft engine.
	ctEnabled := cfg.CTEnabled

	rep := iface.RepresentorDevice
	if rep == "" {
		return nil, fmt.Errorf("interface %q has no resolved representor device", iface.InterfaceName)
	}

	ingressPolicies, egressPolicies := selectPolicies(policyMap, pod, podInfo)

	var cands []candidate

	for _, sp := range ingressPolicies {
		for idx, rule := range sp.policy.Spec.Ingress {
			c, err := expandRule(ctx, deps, rep, DirIngress, sp, idx, rule.Ports, rule.From, podInfo, ctEnabled)
			if err != nil {
				return nil, err
			}
			cands = append(cands, c...)
		}
	}
	for _, sp := range egressPolicies {
		for idx, rule := range sp.policy.Spec.Egress {
			c, err := expandRule(ctx, deps, rep, DirEgress, sp, idx, rule.Ports, rule.To, podInfo, ctEnabled)
			if err != nil {
				return nil, err
			}
			cands = append(cands, c...)
		}
	}

	// default-deny catch-all per direction that carries any policy, plus (when
	// CT is enabled) the chain-0 conntrack dispatch/stateful-accept rules that
	// sit in front of the per-policy chain.
	if len(ingressPolicies) > 0 {
		cands = append(cands, defaultDeny(rep, DirIngress, ctEnabled))
		if ctEnabled {
			cands = append(cands, ctEntryRules(rep, DirIngress)...)
		}
	}
	if len(egressPolicies) > 0 {
		cands = append(cands, defaultDeny(rep, DirEgress, ctEnabled))
		if ctEnabled {
			cands = append(cands, ctEntryRules(rep, DirEgress)...)
		}
	}

	return assignPriorities(cands), nil
}

// ctZoneFor derives a stable, non-zero conntrack zone for a representor and
// direction so connections tracked on different reps/directions do not collide
// in the shared eSwitch conntrack table. The two directions of the same rep get
// distinct zones (the low bit encodes the direction) so pod-ingress and
// pod-egress flows are tracked independently.
func ctZoneFor(rep string, dir Direction) uint16 {
	h := fnv.New32a()
	_, _ = fmt.Fprint(h, rep)
	// Fold the 32-bit hash into 15 bits and use the low bit for direction,
	// keeping the zone within uint16 and non-zero.
	base := uint16(h.Sum32()&0x7fff) << 1
	zone := base | uint16(dir)
	if zone == 0 {
		zone = 1
	}
	return zone
}

// ctEntryRules builds the chain-0 (ctEntryChain) conntrack pipeline for a
// direction. Ordered by tier (lower prio evaluated first):
//   - +trk+inv               -> drop (defensive: reject packets conntrack
//     could not classify, mirroring nft's implicit invalid handling).
//   - +trk+est / +trk+rel    -> accept (stateful return traffic; mirrors the
//     nft backend's allowConntracked "ct state related,established accept").
//   - -trk (untracked)       -> ct(zone) pipe + goto ctPolicyChain: run the
//     packet through conntrack, then evaluate the per-policy rules.
//
// NEW packets fall through conntrack as untracked on first sight, get dispatched
// by the -trk rule, and are then matched (and committed) by the policy chain.
func ctEntryRules(rep string, dir Direction) []candidate {
	zone := ctZoneFor(rep, dir)
	base := FlowerRule{Rep: rep, Direction: dir, Chain: ctEntryChain, HasCTState: true, CTZone: zone}

	invalid := base
	invalid.CTState = ctStateTracked | ctStateInvalid
	invalid.CTStateMask = ctStateTracked | ctStateInvalid
	invalid.Verdict = VerdictDrop

	established := base
	established.CTState = ctStateTracked | ctStateEstablished
	established.CTStateMask = ctStateTracked | ctStateEstablished
	established.Verdict = VerdictAccept

	related := base
	related.CTState = ctStateTracked | ctStateRelated
	related.CTStateMask = ctStateTracked | ctStateRelated
	related.Verdict = VerdictAccept

	dispatch := base
	dispatch.CTState = 0
	dispatch.CTStateMask = ctStateTracked // match -trk (tracked bit examined, value 0)
	dispatch.CTDispatch = true
	dispatch.GotoChain = ctPolicyChain
	dispatch.Verdict = VerdictAccept // unused for a dispatch rule; goto-chain is the control action

	return []candidate{
		{rule: invalid, tier: tierCTInvalid},
		{rule: established, tier: tierCTEstRel},
		{rule: related, tier: tierCTEstRel},
		{rule: dispatch, tier: tierCTDispatch},
	}
}

// defaultDeny builds the lowest-precedence catch-all drop for a direction. When
// CT is enabled it lives in the post-conntrack policy chain (ctPolicyChain).
func defaultDeny(rep string, dir Direction, ctEnabled bool) candidate {
	return candidate{
		rule: FlowerRule{Rep: rep, Direction: dir, Chain: policyChain(ctEnabled), Verdict: VerdictDrop},
		tier: tierDefault,
	}
}

// policyChain returns the chain the per-policy allow/except/default-deny rules
// live in: the post-conntrack chain when CT is enabled, else chain 0.
func policyChain(ctEnabled bool) uint32 {
	if ctEnabled {
		return ctPolicyChain
	}
	return ctEntryChain
}

// expandRule expands a single ingress/egress rule (ports × peers) into
// candidates for the given direction.
func expandRule(ctx context.Context, deps controllers.PolicyDeps, rep string, dir Direction,
	sp selectedPolicy, ruleIdx int, ports []multiv1beta1.MultiNetworkPolicyPort,
	peers []multiv1beta1.MultiNetworkPolicyPeer, podInfo *controllers.PodInfo, ctEnabled bool) ([]candidate, error) {
	_ = ruleIdx // ruleIdx is not needed for priority (mask-shape keyed); kept for readability/parity.

	policyID := sp.policy.GetNamespace() + "/" + sp.policy.GetName()

	portMatches, err := expandPorts(ports)
	if err != nil {
		return nil, fmt.Errorf("policy %q: %w", policyID, err)
	}

	// Expand peers into allow CIDRs and (higher-priority) except CIDRs.
	// An empty peers list means "match any address".
	type peerAddrs struct {
		allow  []string // CIDRs to accept ("" = any)
		except []string // CIDRs to drop (higher priority)
	}
	var expanded []peerAddrs
	if len(peers) == 0 {
		expanded = append(expanded, peerAddrs{allow: []string{""}})
	} else {
		for _, peer := range peers {
			switch {
			case peer.IPBlock != nil:
				allow := v4CIDRs([]string{peer.IPBlock.CIDR})
				except := v4CIDRs(peer.IPBlock.Except)
				expanded = append(expanded, peerAddrs{allow: allow, except: except})
			case peer.PodSelector != nil || peer.NamespaceSelector != nil:
				ips, err := resolvePeerIPv4s(ctx, deps, peer, podInfo, sp.policyNetworks)
				if err != nil {
					return nil, fmt.Errorf("policy %q: resolve selector peer: %w", policyID, err)
				}
				var cidrs []string
				for _, ip := range ips {
					cidrs = append(cidrs, ip+"/32")
				}
				expanded = append(expanded, peerAddrs{allow: cidrs})
			default:
				return nil, fmt.Errorf("policy %q: peer has neither ipBlock nor selector", policyID)
			}
		}
	}

	var out []candidate
	for _, pm := range portMatches {
		for _, pa := range expanded {
			for _, ex := range pa.except {
				out = append(out, candidate{
					rule:     buildRule(rep, dir, pm, ex, VerdictDrop, ctEnabled),
					tier:     tierExcept,
					policyID: policyID,
				})
			}
			for _, cidr := range pa.allow {
				out = append(out, candidate{
					rule:     buildRule(rep, dir, pm, cidr, VerdictAccept, ctEnabled),
					tier:     tierAllow,
					policyID: policyID,
				})
			}
		}
	}
	return out, nil
}

// buildRule assembles a per-policy FlowerRule, placing the peer CIDR in the src
// slot for policy ingress and the dst slot for policy egress.
//
// When CT is enabled the rule lives in the post-conntrack policy chain, and an
// accept additionally commits the NEW connection (CTCommit) so its reply
// direction is tracked and statefully accepted by the chain-0 est/rel rules.
// Drops (except-carve-outs, default-deny) never commit.
func buildRule(rep string, dir Direction, pm portMatch, cidr string, verdict Verdict, ctEnabled bool) FlowerRule {
	r := FlowerRule{
		Rep:       rep,
		Direction: dir,
		Chain:     policyChain(ctEnabled),
		Proto:     pm.proto,
		HasPort:   pm.hasPort,
		PortMin:   pm.portMin,
		PortMax:   pm.portMax,
		Verdict:   verdict,
	}
	if ctEnabled && verdict == VerdictAccept {
		r.CTCommit = true
		r.CTZone = ctZoneFor(rep, dir)
	}
	if cidr != "" {
		if dir == DirIngress {
			r.SrcCIDR = cidr
		} else {
			r.DstCIDR = cidr
		}
	}
	return r
}

// portMatch is one expanded L4 match: a protocol plus an optional destination
// port (single or inclusive range).
type portMatch struct {
	proto   uint8 // 0 = any L4
	hasPort bool
	portMin uint16
	portMax uint16
}

// expandPorts turns a policy port list into portMatches. An empty list yields a
// single match-any-L4 entry. A port entry with a protocol but no port number
// yields a protocol-only match (all ports of that protocol).
func expandPorts(ports []multiv1beta1.MultiNetworkPolicyPort) ([]portMatch, error) {
	if len(ports) == 0 {
		return []portMatch{{}}, nil
	}
	var out []portMatch
	for _, p := range ports {
		proto := protoToNumber(p.Protocol)
		if p.Port == nil {
			// protocol-only match (all ports).
			out = append(out, portMatch{proto: proto})
			continue
		}
		min, err := portNumber(p.Port)
		if err != nil {
			return nil, err
		}
		max := min
		if p.EndPort != nil && *p.EndPort > int32(min) {
			if *p.EndPort < 1 || *p.EndPort > 65535 {
				return nil, fmt.Errorf("endPort %d out of range [1,65535]", *p.EndPort)
			}
			max = uint16(*p.EndPort)
		}
		out = append(out, portMatch{proto: proto, hasPort: true, portMin: min, portMax: max})
	}
	return out, nil
}

// protoToNumber maps a k8s protocol to its IP protocol number, defaulting to
// TCP (mirrors pkg/server.getProtocolInfo).
func protoToNumber(p *corev1.Protocol) uint8 {
	if p == nil {
		return ipProtoTCP
	}
	switch *p {
	case corev1.ProtocolUDP:
		return ipProtoUDP
	case corev1.ProtocolSCTP:
		return ipProtoSCTP
	default:
		return ipProtoTCP
	}
}

// portNumber extracts a numeric port from an IntOrString, accepting numeric
// string ports and rejecting non-numeric named ports (mirrors
// pkg/server.validatePortSpec).
func portNumber(p *intstr.IntOrString) (uint16, error) {
	var num int
	if p.Type == intstr.String {
		n, err := strconv.Atoi(p.StrVal)
		if err != nil {
			return 0, fmt.Errorf("named port %q is not supported; numeric ports are required", p.StrVal)
		}
		num = n
	} else {
		num = p.IntValue()
	}
	if num < 1 || num > 65535 {
		return 0, fmt.Errorf("port %d out of range [1,65535]", num)
	}
	return uint16(num), nil
}

// v4CIDRs returns the subset of cidrs that are valid IPv4 prefixes, canonicalized
// to their masked network form. IPv6 prefixes are dropped (TODO Phase 3).
func v4CIDRs(cidrs []string) []string {
	var out []string
	for _, c := range cidrs {
		ip, ipnet, err := net.ParseCIDR(c)
		if err != nil {
			continue
		}
		if ip.To4() == nil {
			continue // IPv6 — skipped in this phase.
		}
		out = append(out, ipnet.String())
	}
	return out
}

// resolvePeerIPv4s resolves a podSelector/namespaceSelector peer to the set of
// peer pod IPv4 addresses on the policy networks. It mirrors
// pkg/server.applyPolicyPeersRulesSelector but collects only the PEER pod
// interface IPs (the addresses we must match), filtered to IPv4.
func resolvePeerIPv4s(ctx context.Context, deps controllers.PolicyDeps, peer multiv1beta1.MultiNetworkPolicyPeer,
	podInfo *controllers.PodInfo, policyNetworks []string) ([]string, error) {
	podSelector := labels.Everything()
	if peer.PodSelector != nil {
		s, err := metav1.LabelSelectorAsSelector(peer.PodSelector)
		if err != nil {
			return nil, fmt.Errorf("pod selector: %w", err)
		}
		podSelector = s
	}

	var nsSelector labels.Selector
	if peer.NamespaceSelector != nil {
		s, err := metav1.LabelSelectorAsSelector(peer.NamespaceSelector)
		if err != nil {
			return nil, fmt.Errorf("namespace selector: %w", err)
		}
		nsSelector = s
	}

	pods, err := deps.ListPods(ctx, podSelector)
	if err != nil {
		return nil, fmt.Errorf("pod list failed: %w", err)
	}

	// The target pod must itself be attached to one of the policy networks,
	// mirroring the nft engine's gate.
	targetOnNet := false
	for _, intf := range podInfo.Interfaces {
		if intf.CheckPolicyNetwork(policyNetworks) {
			targetOnNet = true
			break
		}
	}
	if !targetOnNet {
		return nil, nil
	}

	seen := make(map[string]struct{})
	var ips []string
	for _, sPod := range pods {
		nsInfo, err := deps.GetNamespaceInfo(ctx, sPod.Namespace)
		if err != nil {
			continue
		}
		if nsSelector != nil && !nsSelector.Matches(labels.Set(nsInfo.Labels)) {
			continue
		}
		sPodInfo, err := deps.GetPodInfo(ctx, sPod)
		if err != nil {
			continue
		}
		for _, sIntf := range sPodInfo.Interfaces {
			if !sIntf.CheckPolicyNetwork(policyNetworks) {
				continue
			}
			for _, ip := range sIntf.IPs {
				parsed := net.ParseIP(ip)
				if parsed == nil || parsed.To4() == nil {
					continue // skip empty / IPv6 (TODO Phase 3).
				}
				canonical := parsed.String()
				if _, ok := seen[canonical]; ok {
					continue
				}
				seen[canonical] = struct{}{}
				ips = append(ips, canonical)
			}
		}
	}
	slices.Sort(ips)
	return ips, nil
}

// assignPriorities deterministically allocates tc filter priorities.
//
// Scheme: within each (direction, chain) — an independent tc priority space,
// since ingress/egress are different qdisc parents and each chain numbers its
// preferences independently — rules are grouped into priority CLASSES keyed by
// (tier, mask-shape, policy identity). The classes are sorted by that key and
// numbered from 1. Consequences:
//   - tc flower's "one mask per priority" rule is honored: a class shares a
//     single mask shape, so all filters at a priority use the same mask (only
//     their VALUES differ, e.g. several /32 peer addresses).
//   - two rules whose address prefix lengths differ have different mask shapes
//     and therefore always land on different priorities.
//   - tier ordering (except < allow < default-deny) guarantees except drops
//     precede the allows they carve out, and default-deny is evaluated last.
//   - the mapping is a pure function of the candidate set, so repeated calls
//     yield identical priorities (stability).
//
// The mask-shape signature includes the chain and ct_state shape, so chain-0
// CT rules and chain-N policy rules occupy separate priority classes even
// though both directions share a parent.
func assignPriorities(cands []candidate) []FlowerRule {
	type spaceKey struct {
		dir   Direction
		chain uint32
	}
	type classKey struct {
		space    spaceKey
		tier     int
		sig      string
		policyID string
	}

	// Collect distinct classes per (direction, chain) priority space.
	classesBySpace := make(map[spaceKey]map[classKey]struct{})
	keyOf := func(c candidate) classKey {
		return classKey{
			space:    spaceKey{dir: c.rule.Direction, chain: c.rule.Chain},
			tier:     c.tier,
			sig:      maskSignature(c.rule),
			policyID: c.policyID,
		}
	}
	for _, c := range cands {
		k := keyOf(c)
		if classesBySpace[k.space] == nil {
			classesBySpace[k.space] = make(map[classKey]struct{})
		}
		classesBySpace[k.space][k] = struct{}{}
	}

	// Sort each space's classes and assign priorities.
	prio := make(map[classKey]uint16)
	for space := range classesBySpace {
		keys := make([]classKey, 0, len(classesBySpace[space]))
		for k := range classesBySpace[space] {
			keys = append(keys, k)
		}
		slices.SortFunc(keys, func(a, b classKey) int {
			if a.tier != b.tier {
				return a.tier - b.tier
			}
			if c := strings.Compare(a.sig, b.sig); c != 0 {
				return c
			}
			return strings.Compare(a.policyID, b.policyID)
		})
		for i, k := range keys {
			prio[k] = uint16(i + 1)
		}
	}

	// Materialize rules with priorities, dedupe, and sort deterministically.
	seen := make(map[FlowerRule]struct{})
	var out []FlowerRule
	for _, c := range cands {
		r := c.rule
		r.Priority = prio[keyOf(c)]
		if _, ok := seen[r]; ok {
			continue
		}
		seen[r] = struct{}{}
		out = append(out, r)
	}
	slices.SortFunc(out, compareFlowerRule)
	return out
}

// maskSignature captures every aspect of a rule that determines the tc flower
// key MASK: chain, direction, src/dst prefix lengths (-1 when absent), the L4
// protocol, whether/what kind of destination-port match is present, and the
// ct_state mask. Including chain + ct_state mask keeps chain-0 CT rules and
// chain-N policy rules in separate priority classes and honors flower's
// one-mask-per-priority constraint for the ct_state key too.
func maskSignature(r FlowerRule) string {
	portKind := 0
	if r.HasPort {
		if r.PortMin == r.PortMax {
			portKind = 1 // single
		} else {
			portKind = 2 // range
		}
	}
	return fmt.Sprintf("c%d|d%d|s%d|D%d|p%d|k%d|m%d", r.Chain, r.Direction,
		prefixLen(r.SrcCIDR), prefixLen(r.DstCIDR), r.Proto, portKind, r.CTStateMask)
}

// prefixLen returns the CIDR prefix length, or -1 when cidr is empty.
func prefixLen(cidr string) int {
	if cidr == "" {
		return -1
	}
	if i := strings.LastIndex(cidr, "/"); i >= 0 {
		if n, err := strconv.Atoi(cidr[i+1:]); err == nil {
			return n
		}
	}
	return -1
}

// compareFlowerRule provides a total, deterministic ordering for output.
func compareFlowerRule(a, b FlowerRule) int {
	if a.Direction != b.Direction {
		return int(a.Direction) - int(b.Direction)
	}
	if a.Chain != b.Chain {
		return int(a.Chain) - int(b.Chain)
	}
	if a.Priority != b.Priority {
		return int(a.Priority) - int(b.Priority)
	}
	if a.CTStateMask != b.CTStateMask {
		return int(a.CTStateMask) - int(b.CTStateMask)
	}
	if a.CTState != b.CTState {
		return int(a.CTState) - int(b.CTState)
	}
	if a.Verdict != b.Verdict {
		return int(a.Verdict) - int(b.Verdict)
	}
	if c := strings.Compare(a.SrcCIDR, b.SrcCIDR); c != 0 {
		return c
	}
	if c := strings.Compare(a.DstCIDR, b.DstCIDR); c != 0 {
		return c
	}
	if a.Proto != b.Proto {
		return int(a.Proto) - int(b.Proto)
	}
	if a.PortMin != b.PortMin {
		return int(a.PortMin) - int(b.PortMin)
	}
	return int(a.PortMax) - int(b.PortMax)
}

// parentHandle returns the clsact qdisc parent for the rule's direction on the
// VF representor. See the Direction doc for the inversion rationale.
func (d Direction) parentHandle() uint32 {
	if d == DirIngress {
		// policy ingress (to pod) => representor EGRESS parent.
		return core.BuildHandle(0xffff, tc.HandleMinEgress)
	}
	// policy egress (from pod) => representor INGRESS parent.
	return core.BuildHandle(0xffff, tc.HandleMinIngress)
}

// handle derives a stable, non-zero 32-bit tc filter handle from the rule's
// identity so that AddFilter is idempotent and reconcile can key filters by
// (parent, chain, priority, handle) without relying on kernel-assigned handles.
//
// The chain and ct_state / CT-action fields are folded into the hash so that
// two filters that share a numeric priority but live in different chains (e.g.
// a chain-0 CT rule and a chain-N policy rule) — or two chain-0 CT rules whose
// only difference is their ct_state combo — get distinct handles and never
// collide in the reconcile diff key.
func (r FlowerRule) handle() uint32 {
	h := fnv.New32a()
	_, _ = fmt.Fprintf(h, "%d|%d|%d|%d|%s|%s|%t|%d|%d|%d|%t|%d|%d|%t|%d|%d",
		r.Chain, r.Direction, r.Priority, r.Proto, r.SrcCIDR, r.DstCIDR,
		r.HasPort, r.PortMin, r.PortMax, r.Verdict,
		r.HasCTState, r.CTState, r.CTStateMask, r.CTDispatch, r.GotoChain, r.CTZone)
	sum := h.Sum32()
	if sum == 0 {
		sum = 1
	}
	return sum
}

// toObject converts the abstract FlowerRule into a go-tc Object ready to be
// installed on the given representor ifindex. Every filter carries the SkipSw
// flag so it is HARDWARE-ONLY (fail closed: if the NIC cannot offload it, the
// kernel rejects the insertion rather than silently enforcing in software).
func (r FlowerRule) toObject(ifindex int) tc.Object {
	skipSw := tc.SkipSw
	ethType := ethTypeIPv4

	flower := &tc.Flower{
		KeyEthType: &ethType,
		Flags:      &skipSw,
	}

	if r.Proto != 0 {
		proto := r.Proto
		flower.KeyIPProto = &proto
	}

	if r.SrcCIDR != "" {
		if ip, mask, ok := cidrToIPMask(r.SrcCIDR); ok {
			flower.KeyIPv4Src = &ip
			flower.KeyIPv4SrcMask = &mask
		}
	}
	if r.DstCIDR != "" {
		if ip, mask, ok := cidrToIPMask(r.DstCIDR); ok {
			flower.KeyIPv4Dst = &ip
			flower.KeyIPv4DstMask = &mask
		}
	}

	if r.HasPort {
		if r.PortMin == r.PortMax {
			port := r.PortMin
			switch r.Proto {
			case ipProtoUDP:
				flower.KeyUDPDst = &port
			case ipProtoSCTP:
				flower.KeySctpDst = &port
			default:
				flower.KeyTCPDst = &port
			}
		} else {
			min := r.PortMin
			max := r.PortMax
			flower.KeyPortDstMin = &min
			flower.KeyPortDstMax = &max
		}
	}

	// ct_state match (stateful pipeline only): match on (value, mask) so the
	// chain-0 rules can select untracked / established / related / invalid
	// packets. A zero mask (stateless rules) emits no ct_state key.
	if r.HasCTState {
		state := r.CTState
		mask := r.CTStateMask
		flower.KeyCtState = &state
		flower.KeyCtStateMask = &mask
	}

	flower.Actions = r.buildActions()

	// Chain is only stamped when non-zero: chain 0 filters (stateless rules and
	// the CT entry chain) omit the attribute exactly as Phase 2 did.
	attr := tc.Attribute{
		Kind:   "flower",
		Flower: flower,
	}
	if r.Chain != 0 {
		chain := r.Chain
		attr.Chain = &chain
	}

	return tc.Object{
		Msg: tc.Msg{
			Ifindex: uint32(ifindex), //nolint:gosec // ifindex is a small positive netdev index
			Handle:  r.handle(),
			Parent:  r.Direction.parentHandle(),
			Info:    core.FilterInfo(r.Priority, ethTypeIPv4),
		},
		Attribute: attr,
	}
}

// buildActions assembles the ordered tc action list for the filter:
//   - CTDispatch (chain-0 untracked rule): [ct(zone) pipe, gact goto_chain N] —
//     send the packet through conntrack, then jump to the policy chain.
//   - CTCommit accept (chain-N NEW-connection allow): [ct commit(zone), gact
//     pass] — commit the connection so its reply is statefully tracked, then
//     pass.
//   - everything else: a single gact (pass for accept, shot for drop),
//     identical to the stateless Phase 2 output.
func (r FlowerRule) buildActions() *[]*tc.Action {
	switch {
	case r.CTDispatch:
		zone := r.CTZone
		goto_ := r.GotoChain
		actions := []*tc.Action{
			{
				Kind: "ct",
				Ct:   &tc.Ct{Zone: &zone},
				// TC_ACT_PIPE: continue to the next action after conntrack.
				Gact: nil,
			},
			{
				Kind: "gact",
				Gact: &tc.Gact{
					Parms: &tc.GactParms{Action: tcActGotoChain | goto_},
				},
			},
		}
		// The ct action's own control disposition is PIPE (fall through to the
		// next action). go-tc carries the ct action's control in CtParms.Action.
		(actions)[0].Ct.Parms = &tc.CtParms{Action: tcActPipe}
		return &actions

	case r.CTCommit && r.Verdict == VerdictAccept:
		zone := r.CTZone
		commit := ctActCommit
		actions := []*tc.Action{
			{
				Kind: "ct",
				Ct: &tc.Ct{
					Action: &commit,
					Zone:   &zone,
					Parms:  &tc.CtParms{Action: tcActPipe},
				},
			},
			{
				Kind: "gact",
				Gact: &tc.Gact{Parms: &tc.GactParms{Action: tcActOK}},
			},
		}
		return &actions

	default:
		control := tcActOK
		if r.Verdict == VerdictDrop {
			control = tcActShot
		}
		actions := []*tc.Action{
			{
				Kind: "gact",
				Gact: &tc.Gact{
					Parms: &tc.GactParms{Action: control},
				},
			},
		}
		return &actions
	}
}

// cidrToIPMask parses an IPv4 CIDR into its 4-byte network address and mask.
func cidrToIPMask(cidr string) (ip net.IP, mask net.IP, ok bool) {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, nil, false
	}
	v4 := ipnet.IP.To4()
	if v4 == nil {
		return nil, nil, false
	}
	return v4, net.IP(ipnet.Mask), true
}
