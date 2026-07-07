package controller

import (
	multiv1beta1 "github.com/k8snetworkplumbingwg/multi-networkpolicy/pkg/apis/k8s.cni.cncf.io/v1beta1"
	netdefv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// SetupScheme registers all Kubernetes and CNI API types used by the manager.
func SetupScheme(scheme *runtime.Scheme) error {
	if err := multiv1beta1.AddToScheme(scheme); err != nil {
		return err
	}
	if err := netdefv1.AddToScheme(scheme); err != nil {
		return err
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		return err
	}
	return nil
}
