#!/usr/bin/env bats

# Note:
# These test cases, stacked, will create stacked policy rules in one multi-networkpolicy and test the
# traffic policying by ncat (nc) command.

setup_file() {
	cd $BATS_TEST_DIRNAME
	load "common"
	export MANIFEST_FILE="ipblock-list.yml"
	kubectl apply --wait --timeout=${kubewait_timeout} -f "${MANIFEST_FILE}"
	kubectl -n test-ipblock-list wait --for=condition=ready -l app=test-ipblock-list pod --timeout=${kubewait_timeout}
	wait_for_nft_rules "test-ipblock-list" "pod-server" "testnetwork-policy-ipblock-1"
}

setup() {
	cd $BATS_TEST_DIRNAME
	load "common"

	server_net1=$(wait_for_net1_ip "test-ipblock-list" "pod-server")
	client_a_net1=$(wait_for_net1_ip "test-ipblock-list" "pod-client-a")
	client_b_net1=$(wait_for_net1_ip "test-ipblock-list" "pod-client-b")
	client_c_net1=$(wait_for_net1_ip "test-ipblock-list" "pod-client-c")
}

teardown_file() {
	teardown_file_common
}


@test "test-ipblock-list check client-a" {
	# ensure nft rules are present before testing connectivity
	wait_for_nft_rules "test-ipblock-list" "pod-server" "testnetwork-policy-ipblock-1"
	run retry_until_success 5 kubectl -n test-ipblock-list exec pod-client-a -- sh -c "echo x | nc -w 1 ${server_net1} 5555"
	[ "$status" -eq  "0" ]
}

@test "test-ipblock-list check client-b" {
	run retry_until_success 5 kubectl -n test-ipblock-list exec pod-client-b -- sh -c "echo x | nc -w 1 ${server_net1} 5555"
	[ "$status" -eq  "0" ]
}

@test "test-ipblock-list check client-c" {
	# ensure nft rules are fully propagated before testing denial
	wait_for_nft_rules "test-ipblock-list" "pod-server" "testnetwork-policy-ipblock-1"
	run retry_until_deny 30 kubectl -n test-ipblock-list exec pod-client-c -- sh -c "echo x | nc -w 1 ${server_net1} 5555"
	[ "$status" -eq  "0" ]
}
