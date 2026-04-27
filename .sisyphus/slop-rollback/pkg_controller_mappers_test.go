package controller

import (
	"context"
	"testing"

	multiv1beta1 "github.com/k8snetworkplumbingwg/multi-networkpolicy/pkg/apis/k8s.cni.cncf.io/v1beta1"
	netdefv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMapPodToNode(t *testing.T) {
	mapFn := mapPodToNode("ignored")

	requests := mapFn(context.Background(), &corev1.Pod{Spec: corev1.PodSpec{NodeName: "node-a"}})
	if len(requests) != 1 || requests[0].NamespacedName.Name != "node-a" {
		t.Fatalf("scheduled pod request = %#v, want node-a", requests)
	}

	if requests := mapFn(context.Background(), &corev1.Pod{}); len(requests) != 0 {
		t.Fatalf("unscheduled pod request = %#v, want empty", requests)
	}
}

func TestMapPolicyToNode(t *testing.T) {
	mapFn := mapPolicyToNode("node-local")

	requests := mapFn(context.Background(), &multiv1beta1.MultiNetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "p"}})
	if len(requests) != 1 || requests[0].NamespacedName.Name != "node-local" {
		t.Fatalf("policy request = %#v, want node-local", requests)
	}
}

func TestMapNamespaceToNode(t *testing.T) {
	mapFn := mapNamespaceToNode("node-local")

	requests := mapFn(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns"}})
	if len(requests) != 1 || requests[0].NamespacedName.Name != "node-local" {
		t.Fatalf("namespace request = %#v, want node-local", requests)
	}
}

func TestMapNetDefToNode(t *testing.T) {
	mapFn := mapNetDefToNode("node-local")

	requests := mapFn(context.Background(), &netdefv1.NetworkAttachmentDefinition{ObjectMeta: metav1.ObjectMeta{Name: "netdef", Namespace: "ns"}})
	if len(requests) != 1 || requests[0].NamespacedName.Name != "node-local" {
		t.Fatalf("netdef request = %#v, want node-local", requests)
	}
}
