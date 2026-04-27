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
	netdefv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"k8s.io/apimachinery/pkg/types"
)

// NetDefInfo contains information that defines a object.
type NetDefInfo struct {
	Netdef     *netdefv1.NetworkAttachmentDefinition
	PluginType string
}

// Name ...
func (info *NetDefInfo) Name() string { return info.Netdef.Name }

// NetDefMap ...
type NetDefMap map[types.NamespacedName]NetDefInfo
