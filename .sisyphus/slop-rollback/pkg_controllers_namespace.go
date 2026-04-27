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

import "fmt"

// NamespaceInfo contains information that defines a namespace.
type NamespaceInfo struct {
	Name   string
	Labels map[string]string
}

// NamespaceMap ...
type NamespaceMap map[string]NamespaceInfo

// GetNamespaceInfo ...
func (nm *NamespaceMap) GetNamespaceInfo(nsName string) (*NamespaceInfo, error) {
	if nm == nil {
		return nil, fmt.Errorf("not found")
	}
	nsInfo, ok := (*nm)[nsName]
	if ok {
		return &nsInfo, nil
	}

	return nil, fmt.Errorf("not found")
}
