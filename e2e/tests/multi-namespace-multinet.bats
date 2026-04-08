#!/usr/bin/env bats

# Note:
# This test case creates two namespaces, each with a different NetworkAttachmentDefinition
# and two pods per namespace. It tests that MultiNetworkPolicy works correctly across
# different namespaces with different network configurations.

setup_file() {
	cd $BATS_TEST_DIRNAME
	load "common"
	export MANIFEST_FILE="multi-namespace-multinet.yml"
	export CLEANUP_NAMESPACES="test-namespace-a test-namespace-b"
	kubectl apply --wait --timeout=${kubewait_timeout} -f "${MANIFEST_FILE}"
	kubectl -n test-namespace-a wait --all --for=condition=ready pod --timeout=${kubewait_timeout}
	kubectl -n test-namespace-b wait --all --for=condition=ready pod --timeout=${kubewait_timeout}
	wait_for_nft_rules "test-namespace-a" "pod-a-1" "test-multinetwork-policy-namespace-a"
}

setup() {
	cd $BATS_TEST_DIRNAME
	load "common"
	pod_a1_net1=$(wait_for_net1_ip "test-namespace-a" "pod-a-1")
	pod_a2_net1=$(wait_for_net1_ip "test-namespace-a" "pod-a-2")

	pod_b1_net1=$(wait_for_net1_ip "test-namespace-b" "pod-b-1")
	pod_b2_net1=$(wait_for_net1_ip "test-namespace-b" "pod-b-2")
	
}

teardown_file() {
	teardown_file_common
}


@test "Allowed connectivity" {
	# Re-verify nft rules are active before connectivity check
	wait_for_nft_rules "test-namespace-a" "pod-a-1" "test-multinetwork-policy-namespace-a" 30
	run retry_until_success 5 kubectl -n test-namespace-b exec pod-b-1 -- sh -c "echo x | nc -w 1 ${pod_a1_net1} 5555"
	[ "$status" -eq  "0" ]

	run retry_until_success 5 kubectl -n test-namespace-a exec pod-a-1 -- sh -c "echo x | nc -w 1 ${pod_b2_net1} 5555"
	[ "$status" -eq  "0" ]
}

@test "Denied connectivity" {
	# Re-verify nft rules are active before denial checks
	wait_for_nft_rules "test-namespace-a" "pod-a-1" "test-multinetwork-policy-namespace-a" 30

	run retry_until_deny 30 kubectl -n test-namespace-a exec pod-a-1 -- sh -c "echo x | nc -w 1 ${pod_a2_net1} 5555"
	[ "$status" -eq  "0" ]
	
	run retry_until_deny 30 kubectl -n test-namespace-a exec pod-a-1 -- sh -c "echo x | nc -w 1 ${pod_b1_net1} 5555"
	[ "$status" -eq  "0" ]

	run retry_until_deny 30 kubectl -n test-namespace-b exec pod-a-2 -- sh -c "echo x | nc -w 1 ${pod_a1_net1} 5555"
	[ "$status" -eq  "0" ]

	run retry_until_deny 30 kubectl -n test-namespace-b exec pod-b-2 -- sh -c "echo x | nc -w 1 ${pod_a1_net1} 5555"
	[ "$status" -eq  "0" ]
}

@test "Allowed by policy absence" {
	run kubectl -n test-namespace-a exec pod-a-2 -- sh -c "echo x | nc -w 1 ${pod_b1_net1} 5555"
	[ "$status" -eq  "0" ]

	run kubectl -n test-namespace-b exec pod-b-1 -- sh -c "echo x | nc -w 1 ${pod_a2_net1} 5555"
	[ "$status" -eq  "0" ]

	run kubectl -n test-namespace-a exec pod-a-2 -- sh -c "echo x | nc -w 1 ${pod_b2_net1} 5555"
	[ "$status" -eq  "0" ]

	run kubectl -n test-namespace-b exec pod-b-1 -- sh -c "echo x | nc -w 1 ${pod_b2_net1} 5555"
	[ "$status" -eq  "0" ]

	run kubectl -n test-namespace-b exec pod-b-1 -- sh -c "echo x | nc -w 1 ${pod_b2_net1} 5555"
	[ "$status" -eq  "0" ]

	run kubectl -n test-namespace-b exec pod-b-2 -- sh -c "echo x | nc -w 1 ${pod_b1_net1} 5555"
	[ "$status" -eq  "0" ]
}
