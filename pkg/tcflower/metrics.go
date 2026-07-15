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

// Observability for the SR-IOV tc-flower backend.
//
// These metrics are pure Go (no netlink), so this file carries NO build tag and
// compiles on both linux and darwin. The linux apply.go calls the small helpers
// below; the non-linux stub never does, but the symbols still exist so the
// package builds identically everywhere.
//
// Metrics are registered into controller-runtime's Prometheus registry
// (sigs.k8s.io/controller-runtime/pkg/metrics.Registry), which the manager
// already exposes on its /metrics endpoint, so no separate HTTP server is
// needed. Registration is guarded by a sync.Once so importing the package more
// than once (e.g. across test binaries) cannot panic on duplicate collectors.

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

// Apply-error reasons for multinetworkpolicy_tc_filter_apply_errors_total. This
// counter is the key FAIL-CLOSED signal: any increment means a flower filter
// that should be enforcing policy on a VF representor could not be installed or
// removed, so the eSwitch dataplane no longer matches desired state.
const (
	// reasonAdd: AddFilter (tc filter replace) failed for a non-offload reason.
	reasonAdd = "add"
	// reasonDelete: DelFilter failed removing a stale managed filter.
	reasonDelete = "delete"
	// reasonSkipSw: AddFilter was rejected because the NIC could not offload the
	// skip_sw (hardware-only) filter. This is the security-critical case — the
	// filter carries SkipSw, so the kernel refuses software fallback and the rule
	// is simply not enforced.
	reasonSkipSw = "skip_sw"
)

var (
	// filtersInstalled tracks the number of managed flower filters desired (and,
	// on success, installed) per representor and direction. Set from the desired
	// rule count in Apply after a successful reconcile; drops to 0 for a
	// direction when all its policies are removed.
	filtersInstalled = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "multinetworkpolicy_tc_filters_installed",
		Help: "Number of managed tc flower filters desired/installed on a VF representor per direction.",
	}, []string{"representor", "direction"})

	// filterApplyErrors counts filter install/remove failures by representor and
	// reason. reason=skip_sw is the fail-closed offload-rejection signal.
	filterApplyErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "multinetworkpolicy_tc_filter_apply_errors_total",
		Help: "Total tc flower filter apply failures, labeled by representor and reason (add|delete|skip_sw). Any increase means a VF representor is not enforcing desired policy (fail-closed).",
	}, []string{"representor", "reason"})

	// ctConnections is a best-effort gauge of the conntrack-offloaded connection
	// count per representor. It is only meaningful when it can be resolved cheaply
	// via the devlink/ctPreflight path; it is currently left UNSET (no cheap pure
	// path exists — see ctPreflight) and reserved for future devlink wiring.
	ctConnections = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "multinetworkpolicy_tc_ct_connections",
		Help: "Best-effort count of conntrack-offloaded connections per VF representor (unset unless resolvable via devlink).",
	}, []string{"representor"})

	// reconcileDuration observes how long enforcing one representor's filters
	// takes inside Apply (build + ensure-qdisc + reconcile), labeled by
	// representor.
	reconcileDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "multinetworkpolicy_tc_reconcile_duration_seconds",
		Help:    "Duration of enforcing tc flower filters on a single VF representor within Apply.",
		Buckets: prometheus.DefBuckets,
	}, []string{"representor"})

	registerMetricsOnce sync.Once
)

// registerMetrics registers the backend's collectors into controller-runtime's
// Prometheus registry exactly once. Safe to call repeatedly.
func registerMetrics() {
	registerMetricsOnce.Do(func() {
		ctrlmetrics.Registry.MustRegister(
			filtersInstalled,
			filterApplyErrors,
			ctConnections,
			reconcileDuration,
		)
	})
}

func init() { registerMetrics() }

// setFiltersInstalled records the desired/installed filter count for a
// representor+direction.
func setFiltersInstalled(rep, direction string, n int) {
	filtersInstalled.WithLabelValues(rep, direction).Set(float64(n))
}

// incFilterApplyError bumps the fail-closed apply-error counter.
func incFilterApplyError(rep, reason string) {
	filterApplyErrors.WithLabelValues(rep, reason).Inc()
}

// observeReconcileDuration records the time spent enforcing one representor.
func observeReconcileDuration(rep string, since time.Time) {
	reconcileDuration.WithLabelValues(rep).Observe(time.Since(since).Seconds())
}

// setCTConnections records the offloaded-connection count for a representor. It
// is reserved for a future devlink-backed CT introspection path (see
// ctPreflight); referenced here so the helper is retained without tripping the
// unused-symbol linter.
func setCTConnections(rep string, n int) {
	ctConnections.WithLabelValues(rep).Set(float64(n))
}

var _ = setCTConnections // reserved: wired once devlink CT-conn readout lands.
