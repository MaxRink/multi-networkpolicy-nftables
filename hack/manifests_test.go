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
	"slices"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	runtimeutil "k8s.io/apimachinery/pkg/runtime"
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
			daemonSet := readObject(t, filepath.Join(root, manifest), "DaemonSet", "multi-networkpolicy-ds-amd64")
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

func readObject(t *testing.T, path, kind, name string) *unstructured.Unstructured {
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
		if object.GetKind() == kind && object.GetName() == name {
			return &object
		}
	}
	t.Fatalf("%s %s not found", kind, name)
	return nil
}

// watchedResources lists every resource the manager opens an informer for
// (see NodeReconciler.SetupWithManager and healthSyncRunnable). A missing
// get/list/watch grant makes controller-runtime's cache sync time out at
// startup, so every shipped ClusterRole must grant all of them.
var watchedResources = []struct{ group, resource string }{
	{"", "nodes"},
	{"", "pods"},
	{"", "namespaces"},
	{"k8s.cni.cncf.io", "multi-networkpolicies"},
	{"k8s.cni.cncf.io", "network-attachment-definitions"},
}

func TestClusterRoleGrantsWatchedResources(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	root := filepath.Dir(filepath.Dir(testFile))

	manifests := []string{
		"config/manager/base/rbac.yaml",
		"deploy.yml",
		"demo/deploy.yml",
		"e2e/multi-network-policy-nftables-e2e.yml",
	}
	for _, manifest := range manifests {
		t.Run(manifest, func(t *testing.T) {
			var role rbacv1.ClusterRole
			obj := readObject(t, filepath.Join(root, manifest), "ClusterRole", "multi-networkpolicy")
			if err := runtimeutil.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &role); err != nil {
				t.Fatalf("convert ClusterRole: %v", err)
			}
			for _, w := range watchedResources {
				for _, verb := range []string{"get", "list", "watch"} {
					if !grants(role.Rules, w.group, w.resource, verb) {
						t.Errorf("ClusterRole does not grant %q on %q (group %q)", verb, w.resource, w.group)
					}
				}
			}
		})
	}
}

func grants(rules []rbacv1.PolicyRule, group, resource, verb string) bool {
	for _, r := range rules {
		// resourceNames-restricted rules cannot satisfy cluster-wide list/watch.
		if len(r.ResourceNames) == 0 &&
			(slices.Contains(r.APIGroups, group) || slices.Contains(r.APIGroups, "*")) &&
			(slices.Contains(r.Resources, resource) || slices.Contains(r.Resources, "*")) &&
			(slices.Contains(r.Verbs, verb) || slices.Contains(r.Verbs, "*")) {
			return true
		}
	}
	return false
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
