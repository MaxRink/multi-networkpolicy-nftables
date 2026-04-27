package controller

import (
	"testing"

	multiv1beta1 "github.com/k8snetworkplumbingwg/multi-networkpolicy/pkg/apis/k8s.cni.cncf.io/v1beta1"
	netdefv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestSetupScheme(t *testing.T) {
	scheme := runtime.NewScheme()

	if err := SetupScheme(scheme); err != nil {
		t.Fatalf("SetupScheme() error = %v", err)
	}

	if _, _, err := scheme.ObjectKinds(&multiv1beta1.MultiNetworkPolicy{}); err != nil {
		t.Fatalf("MultiNetworkPolicy not registered: %v", err)
	}
	if _, _, err := scheme.ObjectKinds(&netdefv1.NetworkAttachmentDefinition{}); err != nil {
		t.Fatalf("NetworkAttachmentDefinition not registered: %v", err)
	}
	if _, _, err := scheme.ObjectKinds(&corev1.Pod{}); err != nil {
		t.Fatalf("corev1.Pod not registered: %v", err)
	}
}
