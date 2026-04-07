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

package utils

import "testing"

func TestCheckNodeNameIdentical(t *testing.T) {
	tests := []struct {
		name string
		s1   string
		s2   string
		want bool
	}{
		{name: "identical hostnames without domain", s1: "node1", s2: "node1", want: true},
		{name: "same hostname different domains", s1: "node1.domain.com", s2: "node1.other.com", want: true},
		{name: "one with domain one without", s1: "node1.domain.com", s2: "node1", want: true},
		{name: "different hostnames", s1: "node1", s2: "node2", want: false},
		{name: "different hostnames with domains", s1: "node1.domain.com", s2: "node2.domain.com", want: false},
		{name: "empty strings", s1: "", s2: "", want: true},
		{name: "one empty one not", s1: "", s2: "node1", want: false},
		{name: "same hostname different subdomains", s1: "node1.sub1.domain.com", s2: "node1.sub2.domain.com", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CheckNodeNameIdentical(tt.s1, tt.s2); got != tt.want {
				t.Fatalf("CheckNodeNameIdentical(%q, %q) = %v, want %v", tt.s1, tt.s2, got, tt.want)
			}
		})
	}
}
