/*
Copyright 2020 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"

	netdefv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	pb "k8s.io/cri-api/pkg/apis/runtime/v1"
	k8sutils "k8s.io/cri-client/pkg/util"
)

// RuntimeKind is enum type variable for container runtime
type RuntimeKind string

const (
	// Cri based runtime (e.g. cri-o)
	Cri = "cri"
)

// Set specifies container runtime kind
func (rk *RuntimeKind) Set(s string) error {
	runtime := strings.ToLower(s)
	switch runtime {
	case Cri:
		*rk = RuntimeKind(runtime)
		return nil
	}
	return fmt.Errorf("invalid container-runtime option %q (possible values: \"cri\")", s)
}

// String returns current runtime kind
func (rk RuntimeKind) String() string { return string(rk) }

// Type returns its type, "RuntimeKind"
func (rk RuntimeKind) Type() string { return "RuntimeKind" }

// InterfaceInfo ...
type InterfaceInfo struct {
	NetattachName string
	InterfaceName string
	InterfaceType string
	IPs           []string
}

// CheckPolicyNetwork checks whether given interface is target or not,
// based on policyNetworks
func (info *InterfaceInfo) CheckPolicyNetwork(policyNetworks []string) bool {
	for _, policyNetworkName := range policyNetworks {
		if policyNetworkName == info.NetattachName {
			return true
		}
	}
	return false
}

// PodInfo contains information that defines a pod.
type PodInfo struct {
	Name          string
	Namespace     string
	NetNSPath     string
	NetworkStatus []netdefv1.NetworkStatus
	NodeName      string
	Interfaces    []InterfaceInfo
}

// CheckPolicyNetwork checks whether given pod is target or not,
// based on policyNetworks
func (info *PodInfo) CheckPolicyNetwork(policyNetworks []string) bool {
	for _, intf := range info.Interfaces {
		for _, policyNetworkName := range policyNetworks {
			if policyNetworkName == intf.NetattachName {
				return true
			}
		}
	}
	return false
}

// GetMultusNetIFs ...
func (info *PodInfo) GetMultusNetIFs() []string {
	results := []string{}

	if info != nil && len(info.NetworkStatus) > 0 {
		for _, status := range info.NetworkStatus[1:] {
			results = append(results, status.Interface)
		}
	}
	return results
}

// String ...
func (info *PodInfo) String() string { return fmt.Sprintf("pod:%s", info.Name) }

// IsMultiNetworkpolicyTarget ...
func IsMultiNetworkpolicyTarget(pod *v1.Pod) bool {
	if pod.Status.Phase != v1.PodRunning {
		return false
	}

	if pod.Spec.HostNetwork {
		return false
	}
	return true
}

// PodMap ...
type PodMap map[types.NamespacedName]PodInfo

// GetPodInfo ...
func (pm *PodMap) GetPodInfo(pod *v1.Pod) (*PodInfo, error) {
	if pm == nil || pod == nil {
		return nil, fmt.Errorf("not found")
	}
	namespacedName := types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}

	podInfo, ok := (*pm)[namespacedName]
	if ok {
		return &podInfo, nil
	}

	return nil, fmt.Errorf("not found")
}

// GetCriRuntimeClient retrieves cri grpc client
func GetCriRuntimeClient(runtimeEndpoint, hostPrefix string) (pb.RuntimeServiceClient, *grpc.ClientConn, error) {
	return GetCriRuntimeClientWithContext(context.Background(), runtimeEndpoint, hostPrefix)
}

// GetCriRuntimeClientWithContext retrieves a CRI gRPC client using the supplied context.
func GetCriRuntimeClientWithContext(ctx context.Context, runtimeEndpoint, hostPrefix string) (pb.RuntimeServiceClient, *grpc.ClientConn, error) {
	hostRuntimeEndpoint := fmt.Sprintf("unix://%s%s", hostPrefix, runtimeEndpoint)
	addr, dialer, err := k8sutils.GetAddressAndDialer(hostRuntimeEndpoint)
	if err != nil {
		return nil, nil, err
	}

	target := "passthrough:///" + addr
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(dialer))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create gRPC client for %s: %w", hostRuntimeEndpoint, err)
	}
	conn.Connect()

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	for {
		state := conn.GetState()
		if state == connectivity.Ready {
			break
		}
		if !conn.WaitForStateChange(ctx, state) {
			_ = conn.Close()
			return nil, nil, fmt.Errorf("timed out waiting for gRPC connection to %s to become ready (last state: %s)", hostRuntimeEndpoint, state)
		}
	}

	return pb.NewRuntimeServiceClient(conn), conn, nil
}

// CloseCriConnection closes grpc connection in client
func CloseCriConnection(conn *grpc.ClientConn) error {
	if conn == nil {
		return nil
	}
	return conn.Close()
}
