/*
Copyright 2026 Deutsche Telekom AG.

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

package hack

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
)

func TestHealthPortDeclaredInManifests(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	root := filepath.Dir(filepath.Dir(testFile))

	manifests := []string{
		"config/manager/base/daemonset.yaml",
		"deploy.yml",
		"e2e/multi-network-policy-nftables-e2e.yml",
	}
	for _, manifest := range manifests {
		t.Run(manifest, func(t *testing.T) {
			daemonSet := readDaemonSet(t, filepath.Join(root, manifest))
			containers, found, err := unstructured.NestedSlice(daemonSet.Object, "spec", "template", "spec", "containers")
			if err != nil {
				t.Fatalf("read containers: %v", err)
			}
			if !found {
				t.Fatal("DaemonSet has no containers")
			}

			for _, rawContainer := range containers {
				container, ok := rawContainer.(map[string]interface{})
				if !ok {
					continue
				}
				if container["name"] != "multi-networkpolicy" {
					continue
				}
				assertHealthPort(t, container)
				return
			}
			t.Fatal("multi-networkpolicy container not found")
		})
	}
}

func readDaemonSet(t *testing.T, path string) *unstructured.Unstructured {
	t.Helper()

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open manifest: %v", err)
	}
	defer file.Close()

	decoder := utilyaml.NewYAMLOrJSONDecoder(file, 4096)
	for {
		var object unstructured.Unstructured
		if err := decoder.Decode(&object); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("decode manifest: %v", err)
		}
		if object.GetKind() == "DaemonSet" && object.GetName() == "multi-networkpolicy-ds-amd64" {
			return &object
		}
	}
	t.Fatalf("DaemonSet multi-networkpolicy-ds-amd64 not found")
	return nil
}

func assertHealthPort(t *testing.T, container map[string]interface{}) {
	t.Helper()

	ports, ok := container["ports"].([]interface{})
	if !ok {
		t.Fatal("multi-networkpolicy container has no ports")
	}
	for _, rawPort := range ports {
		port, ok := rawPort.(map[string]interface{})
		if !ok {
			continue
		}
		if port["name"] == "health" &&
			port["containerPort"] == int64(8081) &&
			port["hostPort"] == int64(8081) &&
			port["protocol"] == "TCP" {
			return
		}
	}
	t.Fatalf("health port declaration missing or incorrect: %#v", ports)
}
