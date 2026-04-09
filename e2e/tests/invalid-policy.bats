#!/usr/bin/env bats

# Note:
# These test cases verify that an invalid or malformed MultiNetworkPolicy
# (e.g. one referencing a non-existent network) does not crash the daemon
# or interfere with valid policies applied to other pods.

setup_file() {
	cd $BATS_TEST_DIRNAME
	load "common"
	export MANIFEST_FILE="invalid-policy.yml"

	# capture baseline restart counts for all daemon pods before applying the policy
	kubectl get pods -l app=multi-networkpolicy -n kube-system \
		-o jsonpath='{range .items[*]}{.metadata.name}={.status.containerStatuses[0].restartCount}{"\n"}{end}' \
		> ${BATS_TMPDIR}/bats-restart-baseline-invalid-policy.txt

	# create test manifests (includes both a valid and an invalid policy)
	kubectl apply --wait --timeout=${kubewait_timeout} -f "${MANIFEST_FILE}"

	# verify all pods are available
	kubectl -n test-invalid-policy wait --for=condition=ready -l app=test-invalid-policy pod --timeout=${kubewait_timeout}

	# wait for nft rules to be programmed by the daemon
	wait_for_nft_rule "test-invalid-policy" "pod-server" "test-multinetwork-policy-invalid-valid-1"
}

setup() {
	cd $BATS_TEST_DIRNAME
	load "common"
	server_net1=$(wait_for_net1_ip "test-invalid-policy" "pod-server")
}

teardown_file() {
	teardown_file_common
}

@test "invalid-policy daemon pods are still running after applying invalid policy" {
	# the daemon must not crash when an invalid policy is present
	run kubectl -n kube-system wait --for=condition=ready -l app=multi-networkpolicy pod --timeout=${kubewait_timeout}
	[ "$status" -eq "0" ]

	# verify no restarts occurred compared to baseline
	# guard: baseline file must be non-empty (at least one daemon pod must exist)
	[ -s "${BATS_TMPDIR}/bats-restart-baseline-invalid-policy.txt" ]
	while IFS='=' read -r pod_name baseline_count; do
		current_count=$(kubectl get pod "$pod_name" -n kube-system \
			-o jsonpath='{.status.containerStatuses[0].restartCount}')
		[ "$current_count" -eq "$baseline_count" ]
	done < ${BATS_TMPDIR}/bats-restart-baseline-invalid-policy.txt
}

@test "invalid-policy valid policy nft rules still exist on pod-server" {
	# the valid policy's nft rules must be present on pod-server despite the invalid policy
	run kubectl -n test-invalid-policy exec pod-server -- sh -c "nft list ruleset | grep test-multinetwork-policy-invalid-valid-1"
	[ "$status" -eq "0" ]
	# client pods should NOT have nft rules
	run kubectl -n test-invalid-policy exec pod-client-a -- sh -c "nft list ruleset | grep test-multinetwork-policy-invalid-valid-1"
	[ "$status" -eq "1" ]
	run kubectl -n test-invalid-policy exec pod-client-b -- sh -c "nft list ruleset | grep test-multinetwork-policy-invalid-valid-1"
	[ "$status" -eq "1" ]
}

@test "invalid-policy check client-a -> server allowed by valid policy" {
	# nc should succeed from client-a to server (allowed by the valid policy)
	run retry_until_success 30 kubectl -n test-invalid-policy exec pod-client-a -- sh -c "echo x | nc -w 1 ${server_net1} 5555"
	[ "$status" -eq "0" ]
}

@test "invalid-policy check client-b -> server blocked by valid policy" {
	# nc should NOT succeed from client-b to server (not allowed by the valid policy)
	run retry_until_deny 30 kubectl -n test-invalid-policy exec pod-client-b -- sh -c "echo x | nc -w 1 ${server_net1} 5555"
	[ "$status" -eq 0 ]
}
