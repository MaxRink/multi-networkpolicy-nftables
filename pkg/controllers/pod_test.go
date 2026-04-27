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

package controllers_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	. "github.com/telekom/multi-networkpolicy-nftables/pkg/controllers"
)

var _ = Describe("runtime kind", func() {
	It("Check container runtime valid case", func() {
		var runtime RuntimeKind
		Expect(runtime.Set("cri")).To(BeNil())
		Expect(runtime.Set("CRI")).To(BeNil())
	})
	It("Check container runtime option invalid case", func() {
		var runtime RuntimeKind
		Expect(runtime.Set("Foobar")).To(MatchError("invalid container-runtime option \"Foobar\" (possible values: \"cri\")"))
	})
})
