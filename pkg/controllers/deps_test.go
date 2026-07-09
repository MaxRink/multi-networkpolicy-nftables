package controllers

import (
	"context"
	"errors"
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

func (m *mockPolicyDeps) ListPods(_ context.Context, selector labels.Selector) ([]*corev1.Pod, error) {
	m.lastSelect = selector
	return m.pods, m.listErr
}

func (m *mockPolicyDeps) GetNamespaceInfo(_ context.Context, namespace string) (*NamespaceInfo, error) {
	m.lastNS = namespace
	return m.namespace, m.nsErr
}

func (m *mockPolicyDeps) GetPodInfo(_ context.Context, pod *corev1.Pod) (*PodInfo, error) {
	m.lastPod = pod
	return m.podInfo, m.podErr
}

type mockNetDefResolver struct {
	err        error
	pluginType string
	lastCtx    context.Context
	lastName   types.NamespacedName
}

func (m *mockNetDefResolver) GetPluginType(ctx context.Context, namespacedName types.NamespacedName) (string, error) {
	m.lastCtx = ctx
	m.lastName = namespacedName
	return m.pluginType, m.err
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

	ctx := context.Background()

	gotPods, err := deps.ListPods(ctx, selector)
	if err != nil {
		t.Fatalf("ListPods returned error: %v", err)
	}
	if len(gotPods) != 1 || gotPods[0] != pod {
		t.Fatalf("ListPods returned %#v", gotPods)
	}
	if deps.lastSelect.String() != selector.String() {
		t.Fatalf("ListPods selector mismatch: got %s want %s", deps.lastSelect.String(), selector.String())
	}

	gotNS, err := deps.GetNamespaceInfo(ctx, "ns-a")
	if err != nil {
		t.Fatalf("GetNamespaceInfo returned error: %v", err)
	}
	if gotNS == nil || gotNS.Name != "ns-a" {
		t.Fatalf("GetNamespaceInfo returned %#v", gotNS)
	}
	if deps.lastNS != "ns-a" {
		t.Fatalf("GetNamespaceInfo namespace mismatch: got %q", deps.lastNS)
	}

	gotPodInfo, err := deps.GetPodInfo(ctx, pod)
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
	gotPlugin, err := resolver.GetPluginType(ctx, types.NamespacedName{Namespace: "ns-a", Name: "net-a"})
	if err != nil {
		t.Fatalf("GetPluginType returned error: %v", err)
	}
	if gotPlugin != "bridge" {
		t.Fatalf("GetPluginType returned %q", gotPlugin)
	}
	if resolver.lastCtx != ctx {
		t.Fatalf("GetPluginType context mismatch: got %#v want %#v", resolver.lastCtx, ctx)
	}
	if resolver.lastName != (types.NamespacedName{Namespace: "ns-a", Name: "net-a"}) {
		t.Fatalf("GetPluginType name mismatch: got %#v", resolver.lastName)
	}
}

func TestNewPodInfoFromPodPropagatesResolverContextAndErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	wantErr := errors.New("resolver failed")
	resolver := &mockNetDefResolver{err: wantErr}
	pod := podWithNetworkAnnotations()

	_, err := NewPodInfoFromPod(ctx, pod, nil, "other-node", []string{"bridge"}, resolver)
	if !errors.Is(err, wantErr) {
		t.Fatalf("NewPodInfoFromPod() error = %v, want %v", err, wantErr)
	}
	if resolver.lastCtx != ctx {
		t.Fatalf("resolver context mismatch: got %#v want %#v", resolver.lastCtx, ctx)
	}
	if resolver.lastName != (types.NamespacedName{Namespace: "ns-a", Name: "net-a"}) {
		t.Fatalf("resolver name mismatch: got %#v", resolver.lastName)
	}
}

func TestNewPodInfoFromPodBuildsMatchingInterface(t *testing.T) {
	t.Parallel()

	resolver := &mockNetDefResolver{pluginType: "bridge"}
	pod := podWithNetworkAnnotations()

	podInfo, err := NewPodInfoFromPod(context.Background(), pod, nil, "other-node", []string{"bridge"}, resolver)
	if err != nil {
		t.Fatalf("NewPodInfoFromPod() error = %v", err)
	}
	if podInfo == nil {
		t.Fatal("NewPodInfoFromPod() returned nil pod info")
	}
	if len(podInfo.Interfaces) != 1 {
		t.Fatalf("interfaces length = %d, want 1 (%#v)", len(podInfo.Interfaces), podInfo.Interfaces)
	}
	if got := podInfo.Interfaces[0]; got.NetattachName != "net-a" || got.InterfaceName != "net1" || got.InterfaceType != "bridge" {
		t.Fatalf("interface = %#v, want net-a/net1/bridge", got)
	}
}

func podWithNetworkAnnotations() *corev1.Pod {
	const (
		name        = "pod-a"
		nodeName    = "node-a"
		networkName = "net-a"
	)
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "ns-a",
			Annotations: map[string]string{
				"k8s.v1.cni.cncf.io/networks": networkName,
				"k8s.v1.cni.cncf.io/network-status": `[{
					"name": "` + networkName + `",
					"interface": "net1",
					"ips": ["10.0.0.2"]
				}]`,
			},
		},
		Spec: corev1.PodSpec{NodeName: nodeName},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}
}
