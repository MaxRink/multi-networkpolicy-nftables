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

package server

import (
	"testing"

	multiv1beta1 "github.com/k8snetworkplumbingwg/multi-networkpolicy/pkg/apis/k8s.cni.cncf.io/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetEnabledPolicyTypes(t *testing.T) {
	tests := []struct {
		name         string
		policyTypes  []multiv1beta1.MultiPolicyType
		ingressRules []multiv1beta1.MultiNetworkPolicyIngressRule
		egressRules  []multiv1beta1.MultiNetworkPolicyEgressRule
		wantIngress  bool
		wantEgress   bool
	}{
		{
			name:         "a: ingress nil, egress nil — no policy types",
			policyTypes:  nil,
			ingressRules: nil,
			egressRules:  nil,
			wantIngress:  false,
			wantEgress:   false,
		},
		{
			name:         "b: ingress empty slice, egress nil — Ingress in result",
			policyTypes:  nil,
			ingressRules: []multiv1beta1.MultiNetworkPolicyIngressRule{},
			egressRules:  nil,
			wantIngress:  true,
			wantEgress:   false,
		},
		{
			name:         "c: ingress non-empty, egress nil — Ingress in result",
			policyTypes:  nil,
			ingressRules: []multiv1beta1.MultiNetworkPolicyIngressRule{{}},
			egressRules:  nil,
			wantIngress:  true,
			wantEgress:   false,
		},
		{
			name:         "d: ingress nil, egress empty — Egress in result",
			policyTypes:  nil,
			ingressRules: nil,
			egressRules:  []multiv1beta1.MultiNetworkPolicyEgressRule{},
			wantIngress:  false,
			wantEgress:   true,
		},
		{
			name:         "e: ingress nil, egress non-empty — Egress in result",
			policyTypes:  nil,
			ingressRules: nil,
			egressRules:  []multiv1beta1.MultiNetworkPolicyEgressRule{{}},
			wantIngress:  false,
			wantEgress:   true,
		},
		{
			name:         "f: both nil — no types (same as a)",
			policyTypes:  nil,
			ingressRules: nil,
			egressRules:  nil,
			wantIngress:  false,
			wantEgress:   false,
		},
		{
			name:         "policyTypes explicit Ingress only",
			policyTypes:  []multiv1beta1.MultiPolicyType{multiv1beta1.PolicyTypeIngress},
			ingressRules: nil,
			egressRules:  []multiv1beta1.MultiNetworkPolicyEgressRule{{}},
			wantIngress:  true,
			wantEgress:   false,
		},
		{
			name:         "policyTypes explicit Egress only",
			policyTypes:  []multiv1beta1.MultiPolicyType{multiv1beta1.PolicyTypeEgress},
			ingressRules: []multiv1beta1.MultiNetworkPolicyIngressRule{{}},
			egressRules:  nil,
			wantIngress:  false,
			wantEgress:   true,
		},
		{
			name:         "policyTypes explicit both Ingress and Egress",
			policyTypes:  []multiv1beta1.MultiPolicyType{multiv1beta1.PolicyTypeIngress, multiv1beta1.PolicyTypeEgress},
			ingressRules: nil,
			egressRules:  nil,
			wantIngress:  true,
			wantEgress:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := &multiv1beta1.MultiNetworkPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: "default",
				},
				Spec: multiv1beta1.MultiNetworkPolicySpec{
					PolicyTypes: tt.policyTypes,
					Ingress:     tt.ingressRules,
					Egress:      tt.egressRules,
				},
			}

			gotIngress, gotEgress := getEnabledPolicyTypes(policy)
			if gotIngress != tt.wantIngress {
				t.Errorf("getEnabledPolicyTypes() ingress = %v, want %v", gotIngress, tt.wantIngress)
			}
			if gotEgress != tt.wantEgress {
				t.Errorf("getEnabledPolicyTypes() egress = %v, want %v", gotEgress, tt.wantEgress)
			}
		})
	}
}
