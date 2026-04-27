package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	cnitypes "github.com/containernetworking/cni/pkg/types"
	multiv1beta1 "github.com/k8snetworkplumbingwg/multi-networkpolicy/pkg/apis/k8s.cni.cncf.io/v1beta1"
	netdefv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	netdefutils "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/utils"
	"github.com/telekom/multi-networkpolicy-nftables/pkg/controllers"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	pb "k8s.io/cri-api/pkg/apis/runtime/v1"
	klog "k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
)

var _ controllers.PolicyDeps = (*NodeReconciler)(nil)
var _ controllers.NetDefResolver = (*NodeReconciler)(nil)

type NodeReconciler struct {
	NodeName       string
	Client         client.Client
	PolicyDeps     controllers.PolicyDeps
	HostPrefix     string
	NetworkPlugins []string
	CommonCfg      controllers.CommonRuleConfig
	CriClient      pb.RuntimeServiceClient
}

func CleanupOnShutdown(ctx context.Context, r *NodeReconciler, cl client.Client) error {
	return cleanupAllPods(ctx, r, cl)
}

func (r *NodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Node{}, builder.WithPredicates(NodePredicate(r.NodeName))).
		Watches(&corev1.Pod{},
			handler.EnqueueRequestsFromMapFunc(mapPodToNode(r.NodeName)),
			builder.WithPredicates(PodPredicate())).
		Watches(&multiv1beta1.MultiNetworkPolicy{},
			handler.EnqueueRequestsFromMapFunc(mapPolicyToNode(r.NodeName)),
			builder.WithPredicates(PolicyPredicate())).
		Watches(&netdefv1.NetworkAttachmentDefinition{},
			handler.EnqueueRequestsFromMapFunc(mapNetDefToNode(r.NodeName))).
		Watches(&corev1.Namespace{},
			handler.EnqueueRequestsFromMapFunc(mapNamespaceToNode(r.NodeName))).
		Complete(r)
}

func (r *NodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if req.Name != r.NodeName {
		return ctrl.Result{}, nil
	}

	var policyList multiv1beta1.MultiNetworkPolicyList
	if err := r.Client.List(ctx, &policyList); err != nil {
		return ctrl.Result{}, fmt.Errorf("list policies: %w", err)
	}
	policyMap := buildPolicyMap(policyList.Items)

	var podList corev1.PodList
	if err := r.Client.List(ctx, &podList, client.MatchingFields{PodHostnameIndex: r.NodeName}); err != nil {
		return ctrl.Result{}, fmt.Errorf("list pods for node %s: %w", r.NodeName, err)
	}

	deps := r.policyDeps()
	retryNeeded := false
	for i := range podList.Items {
		pod := &podList.Items[i]
		if !controllers.IsMultiNetworkpolicyTarget(pod) {
			continue
		}

		podInfo, err := deps.GetPodInfo(pod)
		if err != nil {
			klog.Errorf("failed to get pod info for %s/%s: %v", pod.Namespace, pod.Name, err)
			retryNeeded = true
			continue
		}
		if podInfo == nil || len(podInfo.Interfaces) == 0 {
			if pod.Status.Phase == corev1.PodRunning {
				klog.V(4).Infof("pod %s/%s is running but has no interfaces yet, will retry", pod.Namespace, pod.Name)
				retryNeeded = true
			}
			continue
		}

		if err := applyRulesForPod(deps, r.CommonCfg, policyMap, pod, podInfo, r.HostPrefix); err != nil {
			klog.Errorf("failed to apply rules for %s/%s: %v", pod.Namespace, pod.Name, err)
		}
	}

	if retryNeeded {
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}
	return ctrl.Result{}, nil
}

func (r *NodeReconciler) policyDeps() controllers.PolicyDeps {
	if r.PolicyDeps != nil {
		return r.PolicyDeps
	}
	return r
}

func (r *NodeReconciler) ListPods(selector labels.Selector) ([]*corev1.Pod, error) {
	var podList corev1.PodList
	if err := r.Client.List(context.Background(), &podList, client.MatchingLabelsSelector{Selector: selector}); err != nil {
		return nil, err
	}
	result := make([]*corev1.Pod, len(podList.Items))
	for i := range podList.Items {
		result[i] = &podList.Items[i]
	}
	return result, nil
}

func (r *NodeReconciler) GetNamespaceInfo(namespace string) (*controllers.NamespaceInfo, error) {
	var ns corev1.Namespace
	if err := r.Client.Get(context.Background(), types.NamespacedName{Name: namespace}, &ns); err != nil {
		return nil, err
	}
	return &controllers.NamespaceInfo{Name: ns.Name, Labels: ns.Labels}, nil
}

func (r *NodeReconciler) GetPodInfo(pod *corev1.Pod) (*controllers.PodInfo, error) {
	if r.CriClient == nil {
		return &controllers.PodInfo{Name: pod.Name, Namespace: pod.Namespace, NodeName: pod.Spec.NodeName}, nil
	}
	podInfo := controllers.NewPodInfoFromPod(pod, r.CriClient, r.NodeName, r.NetworkPlugins, r)
	if podInfo == nil {
		return nil, fmt.Errorf("NewPodInfoFromPod returned nil for pod %s/%s", pod.Namespace, pod.Name)
	}
	return podInfo, nil
}

func (r *NodeReconciler) GetPluginType(namespacedName types.NamespacedName) string {
	return resolvePluginType(r.Client, namespacedName)
}

func resolvePluginType(cl client.Client, namespacedName types.NamespacedName) string {
	var nad netdefv1.NetworkAttachmentDefinition
	if err := cl.Get(context.Background(), namespacedName, &nad); err != nil {
		return ""
	}

	confBytes, err := netdefutils.GetCNIConfig(&nad, "/etc/cni/multus/net.d")
	if err != nil {
		return ""
	}

	netconfList := &cnitypes.NetConfList{}
	if err := json.Unmarshal(confBytes, netconfList); err == nil && len(netconfList.Plugins) > 0 {
		return netconfList.Plugins[0].Type
	}

	netconf := &cnitypes.NetConf{}
	if err := json.Unmarshal(confBytes, netconf); err == nil {
		return netconf.Type
	}

	return ""
}

func buildPolicyMap(policies []multiv1beta1.MultiNetworkPolicy) controllers.PolicyMap {
	pm := make(controllers.PolicyMap, len(policies))
	for i := range policies {
		p := &policies[i]
		pm[types.NamespacedName{Namespace: p.Namespace, Name: p.Name}] = controllers.PolicyInfo{Policy: p}
	}
	return pm
}
