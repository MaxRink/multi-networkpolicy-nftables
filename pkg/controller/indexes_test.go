package controller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPodHostnameIndex(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p1"}, Spec: corev1.PodSpec{NodeName: "node-a"}}
	got := podHostnameExtractor(pod)
	if len(got) != 1 || got[0] != "node-a" {
		t.Fatalf("expected [\"node-a\"], got %v", got)
	}

	unscheduled := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p2"}}
	if podHostnameExtractor(unscheduled) != nil {
		t.Fatalf("expected nil for unscheduled pod")
	}
}
