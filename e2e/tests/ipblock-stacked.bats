#!/usr/bin/env bats

# Note:
# These test cases, stacked, will create stacked policy rules in one multi-networkpolicy and test the
# traffic policying by ncat (nc) command.

setup_file() {
	cd $BATS_TEST_DIRNAME
	load "common"
	export MANIFEST_FILE="ipblock-stacked.yml"
	kubectl apply --wait --timeout=${kubewait_timeout} -f "${MANIFEST_FILE}"
	kubectl -n test-ipblock-stacked wait --for=condition=ready -l app=test-ipblock-stacked pod --timeout=${kubewait_timeout}
	wait_for_nft_rules "test-ipblock-stacked" "pod-server" "testnetwork-policy-ipblock-stacked-1"
	sleep 2
}

setup() {
	cd $BATS_TEST_DIRNAME
	load "common"

	server_net1=$(wait_for_net1_ip "test-ipblock-stacked" "pod-server")
	client_a_net1=$(wait_for_net1_ip "test-ipblock-stacked" "pod-client-a")
	client_b_net1=$(wait_for_net1_ip "test-ipblock-stacked" "pod-client-b")
	client_c_net1=$(wait_for_net1_ip "test-ipblock-stacked" "pod-client-c")
}

teardown_file() {
	teardown_file_common
}


@test "check generated nftables rules" {
	run kubectl -n test-ipblock-stacked exec pod-server -it -- sh -c "nft list ruleset | grep testnetwork-policy-ipblock-stacked-1"
	[ "$status" -eq  "0" ]
	run kubectl -n test-ipblock-stacked exec pod-client-a -it -- sh -c "nft list ruleset | grep testnetwork-policy-ipblock-stacked-1"
	[ "$status" -eq  "1" ]
	run kubectl -n test-ipblock-stacked exec pod-client-b -it -- sh -c "nft list ruleset | grep testnetwork-policy-ipblock-stacked-1"
	[ "$status" -eq  "1" ]
	run kubectl -n test-ipblock-stacked exec pod-client-c -it -- sh -c "nft list ruleset | grep testnetwork-policy-ipblock-stacked-1"
	[ "$status" -eq  "1" ]
}

@test "test-ipblock-stacked check client-a" {
	run kubectl -n test-ipblock-stacked exec pod-client-a -- sh -c "echo x | nc -w 1 ${server_net1} 5555"
	[ "$status" -eq  "0" ]
}

@test "test-ipblock-stacked check client-b" {
	run kubectl -n test-ipblock-stacked exec pod-client-b -- sh -c "echo x | nc -w 1 ${server_net1} 5555"
	[ "$status" -eq  "0" ]
}

@test "test-ipblock-stacked check client-c" {
	run retry_until_deny 10 kubectl -n test-ipblock-stacked exec pod-client-c -- sh -c "echo x | nc -w 1 ${server_net1} 5555"
	[ "$status" -eq  "0" ]
}
