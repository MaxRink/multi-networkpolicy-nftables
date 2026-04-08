#!/usr/bin/env bats

# Note:
# These test cases, stacked, will create stacked policy rules in one multi-networkpolicy and test the 
# traffic policying by ncat (nc) command. 

setup_file() {
	cd $BATS_TEST_DIRNAME
	load "common"
	export MANIFEST_FILE="stacked.yml"
	kubectl apply --wait --timeout=${kubewait_timeout} -f "${MANIFEST_FILE}"
	kubectl -n test-stacked wait --for=condition=ready -l app=test-stacked pod --timeout=${kubewait_timeout}
	wait_for_nft_rules "test-stacked" "pod-server" "testnetwork-policy-stacked-1" 20
}

setup() {
	cd $BATS_TEST_DIRNAME
	load "common"

	server_net1=$(wait_for_net1_ip "test-stacked" "pod-server")
	client_a_net1=$(wait_for_net1_ip "test-stacked" "pod-client-a")
	client_b_net1=$(wait_for_net1_ip "test-stacked" "pod-client-b")
	client_c_net1=$(wait_for_net1_ip "test-stacked" "pod-client-c")
}

teardown_file() {
	teardown_file_common
}


@test "check generated nft rules" {
	run kubectl -n test-stacked exec pod-server -it -- sh -c "nft list ruleset | grep multi-ingress-test-stacked-testnetwork-policy-stacked-1"
	[ "$status" -eq  "0" ]
	run kubectl -n test-stacked exec pod-client-a -it -- sh -c "nft list ruleset | grep multi-ingress-test-stacked-testnetwork-policy-stacked-1"
	[ "$status" -eq  "1" ]
	run kubectl -n test-stacked exec pod-client-b -it -- sh -c "nft list ruleset | grep multi-ingress-test-stacked-testnetwork-policy-stacked-1"
	[ "$status" -eq  "1" ]
	run kubectl -n test-stacked exec pod-client-c -it -- sh -c "nft list ruleset | grep multi-ingress-test-stacked-testnetwork-policy-stacked-1"
	[ "$status" -eq  "1" ]
}

@test "test-stacked check client-a" {
	run kubectl -n test-stacked exec pod-client-a -- sh -c "echo x | nc -w 1 ${server_net1} 5555"
	[ "$status" -eq  "0" ]
}

@test "test-stacked check client-b" {
	run kubectl -n test-stacked exec pod-client-b -- sh -c "echo x | nc -w 1 ${server_net1} 5555"
	[ "$status" -eq  "0" ]
}

@test "test-stacked check client-c" {
	run retry_until_deny 30 kubectl -n test-stacked exec pod-client-c -- sh -c "echo x | nc -w 1 ${server_net1} 5555"
	[ "$status" -eq  "0" ]
}

