//go:build linux

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

package tcflower

import (
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"k8s.io/klog/v2"
)

// ctMaxConnsRe extracts the integer value from a devlink param show line such as
//
//	pci/0000:03:00.0: name ct_max_offloaded_conns type driver-specific
//	  values:
//	    cmode runtime value 1048576
var ctMaxConnsRe = regexp.MustCompile(`value\s+(\d+)`)

// ctPreflight is a BEST-EFFORT readiness check for the mlx5 conntrack-offload
// (CT) pipeline. Hardware CT offload on CX5+ requires the eSwitch to use the
// "smfs" (software-managed flow steering) mode, and the number of concurrently
// offloaded connections is bounded by the devlink param
// ct_max_offloaded_conns. Neither can be reliably introspected from pure Go
// without devlink/genetlink, so this helper shells out to `devlink` when it is
// available and only LOGS its findings.
//
// It deliberately never returns an error and never blocks enforcement:
//   - CT filters themselves carry SkipSw, so if the NIC cannot actually offload
//     the conntrack pipeline the kernel rejects the filter insertion in
//     reconcile (that is where fail-closed happens).
//   - devlink may be unavailable in the daemon's container, and the daemon must
//     not refuse to enforce policy merely because it cannot self-diagnose.
//
// Phase 4 scope: this is intentionally a logging stub. Turning it into a hard
// gate (parsing devlink params, verifying smfs, comparing live conntrack usage
// against ct_max_offloaded_conns) requires devlink access and CX5+ hardware
// validation, which is out of scope for unit tests.
func ctPreflight(hostPrefix, repName, pciAddress string) {
	klog.V(4).Infof("tcflower CT preflight: representor=%q pci=%q hostPrefix=%q", repName, pciAddress, hostPrefix)

	devlink, err := exec.LookPath("devlink")
	if err != nil {
		klog.Warningf("tcflower CT preflight: `devlink` not found (%v); cannot verify SMFS steering mode "+
			"or ct_max_offloaded_conns for representor %q. Proceeding best-effort: CT filters carry skip_sw, "+
			"so a NIC that cannot offload conntrack will reject the filters at insertion time.", err, repName)
		return
	}

	if pciAddress == "" {
		klog.Warningf("tcflower CT preflight: no PCI address for representor %q; skipping devlink introspection", repName)
		return
	}
	handle := "pci/" + pciAddress

	// Steering mode: `devlink dev param show <handle> name flow_steering_mode`
	// (mlx5). "smfs" is required for CT offload; "dmfs" cannot offload it.
	if out, err := exec.Command(devlink, "dev", "param", "show", handle, "name", "flow_steering_mode").CombinedOutput(); err != nil { //nolint:gosec // handle is a device PCI address, not user input
		klog.Warningf("tcflower CT preflight: could not read flow_steering_mode for %q (%v): %s. "+
			"CT offload requires SMFS; verify manually on CX5+ hardware.", handle, err, strings.TrimSpace(string(out)))
	} else {
		mode := strings.TrimSpace(string(out))
		smfs := strings.Contains(mode, "smfs")
		setCTOffloadReady(repName, smfs)
		if smfs {
			klog.V(2).Infof("tcflower CT preflight: %q flow_steering_mode reports smfs (CT-offload capable)", handle)
		} else {
			klog.Warningf("tcflower CT preflight: %q flow_steering_mode is NOT smfs; CT offload will not work: %s", handle, mode)
		}
	}

	// Max offloaded connections: the hardware conntrack table capacity. Export it
	// as a gauge so operators can alert on approaching the limit (ENOSPC on
	// insertion means new connections are no longer tracked in hardware).
	if out, err := exec.Command(devlink, "dev", "param", "show", handle, "name", "ct_max_offloaded_conns").CombinedOutput(); err != nil { //nolint:gosec // handle is a device PCI address, not user input
		klog.V(2).Infof("tcflower CT preflight: ct_max_offloaded_conns not resolvable for %q (%v): %s", handle, err, strings.TrimSpace(string(out)))
	} else {
		text := strings.TrimSpace(string(out))
		klog.V(2).Infof("tcflower CT preflight: %q %s", handle, text)
		if m := ctMaxConnsRe.FindStringSubmatch(text); m != nil {
			if n, perr := strconv.Atoi(m[1]); perr == nil {
				setCTMaxOffloadedConns(repName, n)
			}
		}
	}
}
