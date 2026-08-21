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
	"os"
	"runtime"
	"strconv"
	"testing"

	"github.com/google/nftables"
	"github.com/vishvananda/netns"
)

const netNsName = "testing"

// skipUnavailableEnvVar, when set to a true value, makes OpenSystemConn skip
// (rather than fail) when the environment lacks kernel nftables support. This
// must be set explicitly by a CI job that knowingly runs on a runner without
// nftables support; everywhere else, a missing nftables socket is a hard test
// failure so CI can't silently report green without exercising real nftables.
const skipUnavailableEnvVar = "NFTEST_SKIP_IF_UNAVAILABLE"

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
	var conn *nftables.Conn
	ns := netns.None()
	cleanupOnFailure := true
	// A reused debug namespace belongs to the caller and must not be deleted.
	namedNamespace := false
	defer func() {
		if !cleanupOnFailure {
			return
		}
		if conn != nil {
			if err := conn.CloseLasting(); err != nil {
				t.Logf("failed to close nftables connection after setup failure: %v", err)
			}
		}
		if ns.IsOpen() {
			if err := ns.Close(); err != nil {
				t.Logf("failed to close netns after setup failure: %v", err)
			}
		}
		if namedNamespace {
			if err := netns.DeleteNamed(netNsName); err != nil {
				t.Logf("failed to delete netns %q after setup failure: %v", netNsName, err)
			}
		}
		runtime.UnlockOSThread()
	}()

	var err error
	if debug {
		ns, err = netns.GetFromName(netNsName)
		if err == nil {
			t.Logf("Reused netns %q %d, %s", netNsName, ns, ns.UniqueId())
		} else {
			ns, err = netns.NewNamed(netNsName)
			if err != nil {
				t.Fatalf("netns.NewNamed(%q) failed: %v", netNsName, err)
			}
			namedNamespace = true
			t.Logf("Created new netns %q %d, %s", netNsName, ns, ns.UniqueId())
		}
	} else {
		ns, err = netns.New()
		if err != nil {
			t.Fatalf("netns.New() failed: %v", err)
		}
	}

	conn, err = nftables.New(nftables.WithNetNSFd(int(ns)), nftables.AsLasting())
	if err != nil {
		failOrSkipUnavailable(t, "nftables.New()", err)
		return nil, ns
	}

	err = checkNftablesLiveness(conn)
	if err != nil {
		failOrSkipUnavailable(t, "ListTablesOfFamily()", err)
		return nil, ns
	}

	cleanupOnFailure = false
	return conn, ns
}

func checkNftablesLiveness(conn *nftables.Conn) error {
	// ListTablesOfFamily succeeds when the family exists but has no tables,
	// unlike ListTableOfFamily, which reports a missing table as an error.
	_, err := conn.ListTablesOfFamily(nftables.TableFamilyINet)
	return err
}

// failOrSkipUnavailable reports that nftables is unavailable in this
// environment. It skips the test if skipUnavailableEnvVar parses to true
// (a CI job must opt into this explicitly); otherwise it fails the test,
// so a misconfigured environment can't cause CI to silently report green
// without ever exercising real nftables.
func failOrSkipUnavailable(t *testing.T, op string, err error) {
	t.Helper()
	msg := "%s failed: %v (nftables not available in this environment)"
	if shouldSkipUnavailable() {
		t.Skipf(msg, op, err)
		return
	}
	t.Fatalf(msg, op, err)
}

func shouldSkipUnavailable() bool {
	skip, err := strconv.ParseBool(os.Getenv(skipUnavailableEnvVar))
	return err == nil && skip
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
