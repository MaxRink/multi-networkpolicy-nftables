/*
Copyright 2025 Deutsche Telekom AG.

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

package nftest

import (
	"runtime"
	"testing"

	"github.com/google/nftables"
	"github.com/vishvananda/netns"
)

const netNsName = "testing"

// OpenSystemConn returns a netlink connection that tests against
// the running kernel in a separate network namespace.
// nftest.CleanupSystemConn() must be called from a defer to cleanup
// created network namespace.
func OpenSystemConn(t *testing.T, enableSysTests, debug bool) (*nftables.Conn, netns.NsHandle) {
	t.Helper()
	if !enableSysTests {
		t.SkipNow()
	}
	// We lock the goroutine into the current thread, as namespace operations
	// such as those invoked by `netns.New()` are thread-local. This is undone
	// in nftest.CleanupSystemConn().
	runtime.LockOSThread()
	var ns netns.NsHandle
	var err error
	if debug {
		ns, err = netns.GetFromName(netNsName)
		if err == nil {
			t.Logf("Reused netns %q %d, %s", netNsName, ns, ns.UniqueId())
		} else {
			ns, err = netns.NewNamed(netNsName)
			t.Logf("Created new netns %q %d, %s", netNsName, ns, ns.UniqueId())
			if err != nil {
				t.Fatalf("netns.NewNamed(%q) failed: %v", netNsName, err)
			}
		}
	} else {
		ns, err = netns.New()
		if err != nil {
			t.Fatalf("netns.New() failed: %v", err)
		}
	}

	c, err := nftables.New(nftables.WithNetNSFd(int(ns)), nftables.AsLasting())
	if err != nil {
		t.Fatalf("nftables.New() failed: %v", err)
	}
	return c, ns
}

func CleanupSystemConn(t *testing.T, newNS netns.NsHandle, debug bool) {
	defer runtime.UnlockOSThread()

	if err := newNS.Close(); err != nil {
		t.Fatalf("newNS.Close() failed: %v", err)
	}
	if debug {
		t.Logf("Preserved netns %q for debugging", netNsName)
		return
	}
	t.Logf("Close netns %v", newNS.UniqueId())
}
