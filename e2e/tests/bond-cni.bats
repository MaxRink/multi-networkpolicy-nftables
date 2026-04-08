#!/usr/bin/env bats

setup_file() {
	cd $BATS_TEST_DIRNAME
	load "common"
	export MANIFEST_FILE="bond-cni.yml"
	kubectl apply --wait --timeout=${kubewait_timeout} -f "${MANIFEST_FILE}"
	kubectl -n bond-testing wait --for=condition=ready -l app=bond-testing pod --timeout=${kubewait_timeout}
	wait_for_nft_rules "bond-testing" "pod-a" "test-multinetwork-policy-bond"
}

setup() {
	cd $BATS_TEST_DIRNAME
	load "common"
	pod_a_net1=$(wait_for_net1_ip "bond-testing" "pod-a")
	pod_b_net1=$(wait_for_net1_ip "bond-testing" "pod-b")
	pod_c_net1=$(wait_for_net1_ip "bond-testing" "pod-c")
}

teardown_file() {
	teardown_file_common
}


@test "bond-testing check pod-b -> pod-a" {
	run kubectl -n bond-testing exec pod-b -- sh -c "echo x | nc -w 1 ${pod_a_net1} 5555"
	[ "$status" -eq  "0" ]
}

@test "bond-testing check pod-c -> pod-a" {
	wait_for_nft_rules "bond-testing" "pod-a" "test-multinetwork-policy-bond" 30
	run retry_until_deny 30 kubectl -n bond-testing exec pod-c -- sh -c "echo x | nc -w 1 ${pod_a_net1} 5555"
	[ "$status" -eq "0" ]
}

@test "bond-testing check pod-a -> pod-b" {
	wait_for_nft_rules "bond-testing" "pod-a" "test-multinetwork-policy-bond" 30
	run retry_until_deny 30 kubectl -n bond-testing exec pod-a -- sh -c "echo x | nc -w 1 ${pod_b_net1} 5555"
	[ "$status" -eq "0" ]
}

@test "bond-testing check pod-a -> pod-c" {
	run kubectl -n bond-testing exec pod-a -- sh -c "echo x | nc -w 1 ${pod_c_net1} 5555"
	[ "$status" -eq  "0" ]
}

