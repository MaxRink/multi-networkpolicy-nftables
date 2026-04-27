package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	netdefv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	netdefutils "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/utils"
	multiutils "github.com/telekom/multi-networkpolicy-nftables/pkg/utils"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	pb "k8s.io/cri-api/pkg/apis/runtime/v1"
	"k8s.io/klog/v2"
)

// GetPodNetNSPath resolves the pod network namespace path via CRI.
func GetPodNetNSPath(criClient pb.RuntimeServiceClient, pod *corev1.Pod) (string, error) {
	netnsPath := ""

	if pod.Status.Phase != corev1.PodRunning {
		return "", fmt.Errorf("pod is not running")
	}

	// get Container netns
	procPrefix := ""
	if len(pod.Status.ContainerStatuses) == 0 {
		return "", fmt.Errorf("no container status")
	}

	containerURI := strings.Split(pod.Status.ContainerStatuses[0].ContainerID, "://")
	if len(containerURI) < 2 {
		return "", fmt.Errorf("no container ID (%s)", pod.Status.ContainerStatuses[0].ContainerID)
	}

	runtimeKind := containerURI[0]
	containerID := containerURI[1]
	switch runtimeKind {
	default:
		if criClient == nil {
			return "", fmt.Errorf("cannot find cri client")
		}
		if len(containerID) > 0 {
			request := &pb.ContainerStatusRequest{
				ContainerId: containerID,
				Verbose:     true,
			}
			rpcCtx, rpcCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer rpcCancel()
			r, err := criClient.ContainerStatus(rpcCtx, request)
			if err != nil {
				return "", fmt.Errorf("cannot get containerStatus: %v", err)
			}

			info := r.GetInfo()
			var infop interface{}
			err = json.Unmarshal([]byte(info["info"]), &infop)
			if err != nil {
				return "", fmt.Errorf("cannot unmarshal containerStatus info: %v", err)
			}
			pid, ok := infop.(map[string]interface{})["pid"].(float64)
			if !ok {
				return "", fmt.Errorf("cannot get pid from containerStatus info")
			}
			netnsPath = fmt.Sprintf("%s/proc/%d/ns/net", procPrefix, int(pid))
		}
	}

	return netnsPath, nil
}

// NewPodInfoFromPod builds PodInfo for a pod using CRI and network definitions.
func NewPodInfoFromPod(pod *corev1.Pod, criClient pb.RuntimeServiceClient, hostname string, networkPlugins []string, netdefResolver NetDefResolver) *PodInfo {
	var statuses []netdefv1.NetworkStatus
	var netnsPath string
	var netifs []InterfaceInfo
	// get network information only if the pod is ready
	if IsMultiNetworkpolicyTarget(pod) {
		networks, err := netdefutils.ParsePodNetworkAnnotation(pod)
		if err != nil {
			if _, ok := err.(*netdefv1.NoK8sNetworkError); !ok {
				klog.Errorf("failed to get pod network annotation: %v", err)
			}
		}
		// parse networkStatus
		statuses, err = netdefutils.GetNetworkStatus(pod)
		if err != nil {
			klog.Errorf("failed to get pod(%s/%s) network status: %v", pod.Namespace, pod.Name, err)
		}

		klog.V(1).Infof("pod:%s/%s %s/%s", pod.Namespace, pod.Name, hostname, pod.Spec.NodeName)

		// get container network namespace
		netnsPath = ""
		if multiutils.CheckNodeNameIdentical(hostname, pod.Spec.NodeName) {
			netnsPath, err = GetPodNetNSPath(criClient, pod)
			if err != nil {
				klog.Errorf("failed to get pod(%s/%s) network namespace: %v", pod.Namespace, pod.Name, err)
			}
			klog.V(8).Infof("NetnsPath: %s", netnsPath)
		}

		// netdefname -> plugin name map
		networkPluginsMap := make(map[types.NamespacedName]string)
		if networks == nil {
			klog.V(8).Infof("%s/%s: NO NET", pod.Namespace, pod.Name)
		} else {
			klog.V(8).Infof("%s/%s: net: %v", pod.Namespace, pod.Name, networks)
		}
		for _, n := range networks {
			namespace := pod.Namespace
			if n.Namespace != "" {
				namespace = n.Namespace
			}
			namespacedName := types.NamespacedName{Namespace: namespace, Name: n.Name}
			klog.V(8).Infof("networkPlugins[%s], %v", namespacedName, netdefResolver.GetPluginType(namespacedName))
			networkPluginsMap[namespacedName] = netdefResolver.GetPluginType(namespacedName)
		}
		klog.Infof("netdef->pluginMap: %v", networkPluginsMap)

		// match it with
		for _, s := range statuses {
			var netNamespace, netName string
			slashItems := strings.Split(s.Name, "/")
			if len(slashItems) == 2 {
				netNamespace = strings.TrimSpace(slashItems[0])
				netName = slashItems[1]
			} else {
				netNamespace = pod.Namespace
				netName = s.Name
			}
			namespacedName := types.NamespacedName{Namespace: netNamespace, Name: netName}

			for _, pluginName := range networkPlugins {
				if networkPluginsMap[namespacedName] == pluginName {
					netifs = append(netifs, InterfaceInfo{
						NetattachName: s.Name,
						InterfaceName: s.Interface,
						InterfaceType: networkPluginsMap[namespacedName],
						IPs:           s.IPs,
					})
				}
			}
		}

		klog.V(6).Infof("Pod: %s/%s netns:%s netIF:%v", pod.Namespace, pod.Name, netnsPath, netifs)
	} else {
		klog.V(1).Infof("Pod:%s/%s %s/%s, not ready", pod.Namespace, pod.Name, hostname, pod.Spec.NodeName)
	}

	slices.SortFunc(netifs, func(a, b InterfaceInfo) int {
		return strings.Compare(a.InterfaceName, b.InterfaceName)
	})

	return &PodInfo{
		Name:          pod.Name,
		Namespace:     pod.Namespace,
		NetworkStatus: statuses,
		NetNSPath:     netnsPath,
		NodeName:      pod.Spec.NodeName,
		Interfaces:    netifs,
	}
}
