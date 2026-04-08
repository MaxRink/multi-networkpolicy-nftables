#!/usr/bin/env bats

setup_file() {
	cd $BATS_TEST_DIRNAME
	load "common"
	export MANIFEST_FILE="port-range.yml"
	kubectl apply --wait --timeout=${kubewait_timeout} -f "${MANIFEST_FILE}"
	kubectl -n test-port-range wait --for=condition=ready -l app=test-port-range pod --timeout=${kubewait_timeout}
	wait_for_nft_rules "test-port-range" "pod-a" "test-multinetwork-policy-simple-1"
}

setup() {
	cd $BATS_TEST_DIRNAME
	load "common"
	pod_a_net1=$(wait_for_net1_ip "test-port-range" "pod-a")
	pod_b_net1=$(wait_for_net1_ip "test-port-range" "pod-b")
}

teardown_file() {
	teardown_file_common
}


@test "test-port-range check pod-a -> pod-b 5555 OK" {
	# nc should succeed from client-a to server by policy
	run kubectl -n test-port-range exec pod-a -- sh -c "echo x | nc -w 2 ${pod_b_net1} 5555"
	[ "$status" -eq  "0" ]
}

@test "test-port-range check pod-a -> pod-b 6666 KO" {
	wait_for_nft_rules "test-port-range" "pod-a" "test-multinetwork-policy-simple-1" 30
	run retry_until_deny 30 kubectl -n test-port-range exec pod-a -- sh -c "echo x | nc -w 2 ${pod_b_net1} 6666"
	[ "$status" -eq "0" ]
}

@test "test-port-range check pod-b -> pod-a 5555 KO" {
	wait_for_nft_rules "test-port-range" "pod-a" "test-multinetwork-policy-simple-1" 30
	run retry_until_deny 30 kubectl -n test-port-range exec pod-b -- sh -c "echo x | nc -w 2 ${pod_a_net1} 5555"
	[ "$status" -eq "0" ]
}

@test "test-port-range check pod-b -> pod-a 6666 OK" {
	# nc should succeed from client-a to server by policy
	run kubectl -n test-port-range exec pod-b -- sh -c "echo x | nc -w 2 ${pod_a_net1} 6666"
	[ "$status" -eq  "0" ]
}

