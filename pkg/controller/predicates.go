package controller

import (
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	corev1 "k8s.io/api/core/v1"
)

// PodPredicate filters pod events: allows Create/Delete, allows Update only if pod phase changed or labels changed.
func PodPredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(event.CreateEvent) bool { return true },
		DeleteFunc: func(event.DeleteEvent) bool { return true },
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldPod, okOld := e.ObjectOld.(*corev1.Pod)
			newPod, okNew := e.ObjectNew.(*corev1.Pod)
			if !okOld || !okNew {
				return false
			}

			return oldPod.Status.Phase != newPod.Status.Phase || labelsChanged(oldPod.Labels, newPod.Labels)
		},
		GenericFunc: func(event.GenericEvent) bool { return false },
	}
}

// PolicyPredicate filters policy events: allows Create/Delete, allows Update only if Generation changed.
func PolicyPredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(event.CreateEvent) bool { return true },
		DeleteFunc: func(event.DeleteEvent) bool { return true },
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration()
		},
		GenericFunc: func(event.GenericEvent) bool { return false },
	}
}

// NodePredicate filters node events to only the named node.
func NodePredicate(nodeName string) predicate.Predicate {
	return predicate.Funcs{
		CreateFunc:  func(e event.CreateEvent) bool { return e.Object.GetName() == nodeName },
		DeleteFunc:  func(e event.DeleteEvent) bool { return e.Object.GetName() == nodeName },
		UpdateFunc:  func(e event.UpdateEvent) bool { return e.ObjectNew.GetName() == nodeName },
		GenericFunc: func(e event.GenericEvent) bool { return e.Object.GetName() == nodeName },
	}
}

func labelsChanged(oldLabels, newLabels map[string]string) bool {
	if len(oldLabels) != len(newLabels) {
		return true
	}

	for key, oldVal := range oldLabels {
		if newVal, ok := newLabels[key]; !ok || newVal != oldVal {
			return true
		}
	}

	return false
}
