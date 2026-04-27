package controllers

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
)

type mockPolicyDeps struct {
	pods       []*corev1.Pod
	namespace  *NamespaceInfo
	podInfo    *PodInfo
	listErr    error
	nsErr      error
	podErr     error
	lastSelect labels.Selector
	lastNS     string
	lastPod    *corev1.Pod
}

func (m *mockPolicyDeps) ListPods(selector labels.Selector) ([]*corev1.Pod, error) {
	m.lastSelect = selector
	return m.pods, m.listErr
}

func (m *mockPolicyDeps) GetNamespaceInfo(namespace string) (*NamespaceInfo, error) {
	m.lastNS = namespace
	return m.namespace, m.nsErr
}

func (m *mockPolicyDeps) GetPodInfo(pod *corev1.Pod) (*PodInfo, error) {
	m.lastPod = pod
	return m.podInfo, m.podErr
}

type mockNetDefResolver struct {
	pluginType string
	lastName   types.NamespacedName
}

func (m *mockNetDefResolver) GetPluginType(namespacedName types.NamespacedName) string {
	m.lastName = namespacedName
	return m.pluginType
}

var _ PolicyDeps = &mockPolicyDeps{}
var _ NetDefResolver = &mockNetDefResolver{}

func TestPolicyDeps(t *testing.T) {
	t.Parallel()

	selector := labels.SelectorFromSet(labels.Set{"app": "demo"})
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-a", Namespace: "ns-a"}}
	deps := &mockPolicyDeps{
		pods:      []*corev1.Pod{pod},
		namespace: &NamespaceInfo{Name: "ns-a", Labels: map[string]string{"team": "net"}},
		podInfo:   &PodInfo{Name: "pod-a"},
	}

	gotPods, err := deps.ListPods(selector)
	if err != nil {
		t.Fatalf("ListPods returned error: %v", err)
	}
	if len(gotPods) != 1 || gotPods[0] != pod {
		t.Fatalf("ListPods returned %#v", gotPods)
	}
	if deps.lastSelect.String() != selector.String() {
		t.Fatalf("ListPods selector mismatch: got %s want %s", deps.lastSelect.String(), selector.String())
	}

	gotNS, err := deps.GetNamespaceInfo("ns-a")
	if err != nil {
		t.Fatalf("GetNamespaceInfo returned error: %v", err)
	}
	if gotNS == nil || gotNS.Name != "ns-a" {
		t.Fatalf("GetNamespaceInfo returned %#v", gotNS)
	}
	if deps.lastNS != "ns-a" {
		t.Fatalf("GetNamespaceInfo namespace mismatch: got %q", deps.lastNS)
	}

	gotPodInfo, err := deps.GetPodInfo(pod)
	if err != nil {
		t.Fatalf("GetPodInfo returned error: %v", err)
	}
	if gotPodInfo == nil || gotPodInfo.Name != "pod-a" {
		t.Fatalf("GetPodInfo returned %#v", gotPodInfo)
	}
	if deps.lastPod != pod {
		t.Fatalf("GetPodInfo pod mismatch: got %#v", deps.lastPod)
	}

	resolver := &mockNetDefResolver{pluginType: "bridge"}
	gotPlugin := resolver.GetPluginType(types.NamespacedName{Namespace: "ns-a", Name: "net-a"})
	if gotPlugin != "bridge" {
		t.Fatalf("GetPluginType returned %q", gotPlugin)
	}
	if resolver.lastName != (types.NamespacedName{Namespace: "ns-a", Name: "net-a"}) {
		t.Fatalf("GetPluginType name mismatch: got %#v", resolver.lastName)
	}
}
