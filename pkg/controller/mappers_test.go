package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestMapPodToNode(t *testing.T) {
	mapFn := mapPodToNode()

	requests := mapFn(context.Background(), &corev1.Pod{Spec: corev1.PodSpec{NodeName: "node-a"}})
	if len(requests) != 1 || requests[0].Name != "node-a" {
		t.Fatalf("scheduled pod request = %#v, want node-a", requests)
	}

	if requests := mapFn(context.Background(), &corev1.Pod{}); len(requests) != 0 {
		t.Fatalf("unscheduled pod request = %#v, want empty", requests)
	}
}
