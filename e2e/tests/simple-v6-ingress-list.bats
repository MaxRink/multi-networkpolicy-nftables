#!/usr/bin/env bats

# Note:
# These test cases, simple, will create simple (one policy for ingress) and test the 
# traffic policying by ncat (nc) command. In addition, these cases also verifies that
# simple nftables generation check by nftables-save and pod-iptable in multi-networkpolicy pod.


setup_file() {
	cd $BATS_TEST_DIRNAME
	load "common"
	export MANIFEST_FILE="simple-v6-ingress-list.yml"
	kubectl apply --wait --timeout=${kubewait_timeout} -f "${MANIFEST_FILE}"
	kubectl -n test-simple-v6-ingress-list wait --for=condition=ready -l app=test-simple-v6-ingress-list pod --timeout=${kubewait_timeout}
	wait_for_nft_rules "test-simple-v6-ingress-list" "pod-server" "test-multinetwork-policy-simple-1"
}

setup() {
	cd $BATS_TEST_DIRNAME
	load "common"
	server_net1=$(get_net1_ip6 "test-simple-v6-ingress-list" "pod-server")
	client_a_net1=$(get_net1_ip6 "test-simple-v6-ingress-list" "pod-client-a")
	client_b_net1=$(get_net1_ip6 "test-simple-v6-ingress-list" "pod-client-b")
	client_c_net1=$(get_net1_ip6 "test-simple-v6-ingress-list" "pod-client-c")
}

teardown_file() {
	teardown_file_common
}


@test "test-simple-v6-ingress-list check client-a -> server" {
	# ensure nft rules are present before testing connectivity
	wait_for_nft_rules "test-simple-v6-ingress-list" "pod-server" "test-multinetwork-policy-simple-1"
	# nc should succeed from client-a to server by policy (retry for IPv6 NDP convergence)
	run retry_until_allow 10 kubectl -n test-simple-v6-ingress-list exec pod-client-a -- sh -c "echo x | nc -w 1 ${server_net1} 5555"
	[ "$status" -eq  "0" ]
}

@test "test-simple-v6-ingress-list check client-b -> server" {
	# ensure nft rules are fully propagated before testing denial
	wait_for_nft_rules "test-simple-v6-ingress-list" "pod-server" "test-multinetwork-policy-simple-1"
	# nc should NOT succeed from client-b to server by policy
	run retry_until_deny 30 kubectl -n test-simple-v6-ingress-list exec pod-client-b -- sh -c "echo x | nc -w 1 ${server_net1} 5555"
	[ "$status" -eq  "0" ]
}

@test "test-simple-v6-ingress-list check client-c -> server" {
	# nc should succeed from client-c to server by policy (retry for IPv6 NDP convergence)
	run retry_until_allow 10 kubectl -n test-simple-v6-ingress-list exec pod-client-c -- sh -c "echo x | nc -w 1 ${server_net1} 5555"
	[ "$status" -eq  "0" ]
}

@test "test-simple-v6-ingress-list check server -> client-a" {
	# nc should succeed from server to client-a by no policy definition for direction (egress for pod-server)
	run retry_until_allow 10 kubectl -n test-simple-v6-ingress-list exec pod-server -- sh -c "echo x | nc -w 1 ${client_a_net1} 5555"
	[ "$status" -eq  "0" ]
}

@test "test-simple-v6-ingress-list check server -> client-b" {
	# nc should succeed from server to client-b by no policy definition for direction (egress for pod-server)
	run retry_until_allow 10 kubectl -n test-simple-v6-ingress-list exec pod-server -- sh -c "echo x | nc -w 1 ${client_b_net1} 5555"
	[ "$status" -eq  "0" ]
}

@test "test-simple-v6-ingress-list check server -> client-c" {
	# nc should succeed from server to client-c by no policy definition for direction (egress for pod-server)
	run retry_until_allow 10 kubectl -n test-simple-v6-ingress-list exec pod-server -- sh -c "echo x | nc -w 1 ${client_c_net1} 5555"
	[ "$status" -eq  "0" ]
}
