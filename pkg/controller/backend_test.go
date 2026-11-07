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
	"fmt"
	"testing"

	"github.com/telekom/multi-networkpolicy-nftables/pkg/controllers"
	"github.com/telekom/multi-networkpolicy-nftables/pkg/tcflower"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

func TestSelectBackend(t *testing.T) {
	macvlan := controllers.InterfaceInfo{NetattachName: "net1", InterfaceName: "net1", InterfaceType: "macvlan"}
	vfPCI := controllers.InterfaceInfo{NetattachName: "sriov-net", InterfaceName: "net1", PCIAddress: "0000:04:00.2"}
	vfRep := controllers.InterfaceInfo{NetattachName: "sriov-net", InterfaceName: "net1", RepresentorDevice: "enp4s0f0_3"}

	tests := []struct {
		name        string
		iface       controllers.InterfaceInfo
		tcDisabled  bool
		checkErr    error
		want        backendKind
		wantChecked bool
		wantReason  string // fallback metric label expected to increment ("" = none)
	}{
		{name: "macvlan interface uses nftables", iface: macvlan, want: backendNFT},
		{name: "switchdev VF (pci) keeps tc", iface: vfPCI, want: backendTC, wantChecked: true},
		{name: "switchdev VF (representor only) keeps tc", iface: vfRep, want: backendTC, wantChecked: true},
		{
			name: "tc backend disabled falls back to nftables without probing", iface: vfPCI, tcDisabled: true,
			want: backendNFT, wantReason: tcflower.FallbackReasonTCDisabled,
		},
		{
			name: "legacy-mode (non-switchdev) VF falls back to nftables", iface: vfPCI,
			checkErr: fmt.Errorf("wrapped: %w", tcflower.ErrNotSwitchdev),
			want:     backendNFT, wantChecked: true, wantReason: "not_switchdev",
		},
		{
			name: "missing representor falls back to nftables", iface: vfRep,
			checkErr: fmt.Errorf("wrapped: %w", tcflower.ErrNoRepresentor),
			want:     backendNFT, wantChecked: true, wantReason: "not_found",
		},
		{
			name: "require without CT offload falls back to nftables", iface: vfPCI,
			checkErr: fmt.Errorf("wrapped: %w", tcflower.ErrCTOffloadUnavailable),
			want:     backendNFT, wantChecked: true, wantReason: "ct_offload_unavailable",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			checked := false
			r := &NodeReconciler{
				HostPrefix:        "/host",
				TCBackendDisabled: tc.tcDisabled,
				TCCheckEnforceableFunc: func(hostPrefix string, _ controllers.CommonRuleConfig, iface controllers.InterfaceInfo) error {
					checked = true
					if hostPrefix != "/host" || iface.PCIAddress != tc.iface.PCIAddress || iface.RepresentorDevice != tc.iface.RepresentorDevice {
						t.Errorf("check called with (%q, %+v)", hostPrefix, iface)
					}
					return tc.checkErr
				},
			}
			var before float64
			if tc.wantReason != "" {
				before = fallbackCount(t, tc.wantReason)
			}
			if got := r.selectBackend(tc.iface); got != tc.want {
				t.Fatalf("selectBackend() = %v, want %v", got, tc.want)
			}
			if checked != tc.wantChecked {
				t.Fatalf("CheckEnforceable called = %v, want %v", checked, tc.wantChecked)
			}
			if tc.wantReason != "" {
				if d := fallbackCount(t, tc.wantReason) - before; d != 1 {
					t.Fatalf("fallback counter{reason=%q} delta = %v, want 1", tc.wantReason, d)
				}
			}
		})
	}
}

// fallbackCount reads multinetworkpolicy_tc_backend_fallback_total{reason}.
func fallbackCount(t *testing.T, reason string) float64 {
	t.Helper()
	mfs, err := ctrlmetrics.Registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != "multinetworkpolicy_tc_backend_fallback_total" {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "reason" && l.GetValue() == reason {
					return m.GetCounter().GetValue()
				}
			}
		}
	}
	return 0
}

// tcIfSRIOV is the pre-fallback routing (every SR-IOV interface to tc), used
// to test partitioning independently of the eligibility probe.
func tcIfSRIOV(iface controllers.InterfaceInfo) backendKind {
	if iface.IsSRIOV() {
		return backendTC
	}
	return backendNFT
}

func TestPartitionByBackend(t *testing.T) {
	nft := controllers.InterfaceInfo{NetattachName: "macvlan-net", InterfaceName: "net1", InterfaceType: "macvlan"}
	tcA := controllers.InterfaceInfo{NetattachName: "sriov-a", InterfaceName: "net2", PCIAddress: "0000:04:00.2"}
	tcB := controllers.InterfaceInfo{NetattachName: "sriov-b", InterfaceName: "net3", RepresentorDevice: "enp4s0f0_5"}

	t.Run("nil pod info", func(t *testing.T) {
		if got := partitionByBackend(nil, tcIfSRIOV); got != nil {
			t.Fatalf("partitionByBackend(nil, tcIfSRIOV) = %v, want nil", got)
		}
	})

	t.Run("no interfaces", func(t *testing.T) {
		if got := partitionByBackend(&controllers.PodInfo{Name: "p"}, tcIfSRIOV); got != nil {
			t.Fatalf("partitionByBackend(empty) = %v, want nil", got)
		}
	})

	t.Run("only nft", func(t *testing.T) {
		got := partitionByBackend(&controllers.PodInfo{Interfaces: []controllers.InterfaceInfo{nft}}, tcIfSRIOV)
		if len(got) != 1 {
			t.Fatalf("got %d groups, want 1", len(got))
		}
		if _, ok := got[backendTC]; ok {
			t.Fatalf("unexpected tc group for nft-only pod")
		}
		if n := len(got[backendNFT].Interfaces); n != 1 {
			t.Fatalf("nft group has %d interfaces, want 1", n)
		}
	})

	t.Run("mixed pod splits into both backends", func(t *testing.T) {
		podInfo := &controllers.PodInfo{
			Name:       "mixed",
			Namespace:  "ns",
			NetNSPath:  "/proc/1/ns/net",
			NodeName:   "node-1",
			Interfaces: []controllers.InterfaceInfo{nft, tcA, tcB},
		}
		got := partitionByBackend(podInfo, tcIfSRIOV)
		if len(got) != 2 {
			t.Fatalf("got %d groups, want 2 (nft + tc)", len(got))
		}

		nftGroup, ok := got[backendNFT]
		if !ok {
			t.Fatal("missing nft group")
		}
		if len(nftGroup.Interfaces) != 1 || nftGroup.Interfaces[0].NetattachName != "macvlan-net" {
			t.Fatalf("nft group interfaces = %+v, want just macvlan-net", nftGroup.Interfaces)
		}

		tcGroup, ok := got[backendTC]
		if !ok {
			t.Fatal("missing tc group")
		}
		if len(tcGroup.Interfaces) != 2 {
			t.Fatalf("tc group has %d interfaces, want 2", len(tcGroup.Interfaces))
		}

		// Scalar PodInfo fields are preserved on each narrowed copy, and the
		// narrowing must not mutate the original slice.
		if tcGroup.Name != "mixed" || tcGroup.NetNSPath != "/proc/1/ns/net" || tcGroup.NodeName != "node-1" {
			t.Fatalf("tc group lost scalar PodInfo fields: %+v", tcGroup)
		}
		if len(podInfo.Interfaces) != 3 {
			t.Fatalf("original PodInfo.Interfaces mutated: len = %d, want 3", len(podInfo.Interfaces))
		}
	})
}

func TestBackendKindString(t *testing.T) {
	if backendNFT.String() != "nftables" {
		t.Fatalf("backendNFT.String() = %q, want nftables", backendNFT.String())
	}
	if backendTC.String() != "tc" {
		t.Fatalf("backendTC.String() = %q, want tc", backendTC.String())
	}
}
