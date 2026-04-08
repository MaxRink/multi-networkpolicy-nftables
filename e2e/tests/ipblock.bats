#!/usr/bin/env bats

# Note:
# These test cases, stacked, will create stacked policy rules in one multi-networkpolicy and test the
# traffic policying by ncat (nc) command.

setup_file() {
	cd $BATS_TEST_DIRNAME
	load "common"
	export MANIFEST_FILE="ipblock.yml"
	kubectl apply --wait --timeout=${kubewait_timeout} -f "${MANIFEST_FILE}"
	kubectl -n test-ipblock wait --for=condition=ready -l app=test-ipblock pod --timeout=${kubewait_timeout}
	wait_for_nft_rules "test-ipblock" "pod-server" "testnetwork-policy-ipblock-1"
}

setup() {
	cd $BATS_TEST_DIRNAME
	load "common"

	server_net1=$(wait_for_net1_ip "test-ipblock" "pod-server")
	client_a_net1=$(wait_for_net1_ip "test-ipblock" "pod-client-a")
	client_b_net1=$(wait_for_net1_ip "test-ipblock" "pod-client-b")
	client_c_net1=$(wait_for_net1_ip "test-ipblock" "pod-client-c")
}

teardown_file() {
	teardown_file_common
}


@test "check generated nft rules" {
	run kubectl -n test-ipblock exec pod-server -it -- sh -c "nft list ruleset | grep testnetwork-policy-ipblock-1"
	[ "$status" -eq  "0" ]
	run kubectl -n test-ipblock exec pod-client-a -it -- sh -c "nft list ruleset | grep testnetwork-policy-ipblock-1"
	[ "$status" -eq  "1" ]
	run kubectl -n test-ipblock exec pod-client-b -it -- sh -c "nft list ruleset | grep testnetwork-policy-ipblock-1"
	[ "$status" -eq  "1" ]
	run kubectl -n test-ipblock exec pod-client-c -it -- sh -c "nft list ruleset | grep testnetwork-policy-ipblock-1"
	[ "$status" -eq  "1" ]
}

@test "test-ipblock check client-a" {
	run kubectl -n test-ipblock exec pod-client-a -- sh -c "echo x | nc -w 1 ${server_net1} 5555"
	[ "$status" -eq  "0" ]
}

@test "test-ipblock check client-b" {
	run kubectl -n test-ipblock exec pod-client-b -- sh -c "echo x | nc -w 1 ${server_net1} 5555"
	[ "$status" -eq  "0" ]
}

@test "test-ipblock check client-c" {
	run retry_until_deny 30 kubectl -n test-ipblock exec pod-client-c -- sh -c "echo x | nc -w 1 ${server_net1} 5555"
	[ "$status" -eq  "0" ]
}
