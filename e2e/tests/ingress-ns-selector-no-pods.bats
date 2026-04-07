#!/usr/bin/env bats


# Note:
# These test cases verify that an ingress rule on the server that matches no pods 
# does not allow any pods to reach the server.

setup_file() {
	cd $BATS_TEST_DIRNAME
	load "common"
	export MANIFEST_FILE="ingress-ns-selector-no-pods.yml"
	kubectl apply --wait --timeout=${kubewait_timeout} -f "${MANIFEST_FILE}"
	kubectl -n test-ingress-ns-selector-no-pods wait --for=condition=ready -l app=test-ingress-ns-selector-no-pods pod --timeout=${kubewait_timeout}
	wait_for_nft_rules "test-ingress-ns-selector-no-pods" "pod-server" "policy-test-ingress-ns-selector-no-pods"
	sleep 2
}

setup() {
	cd $BATS_TEST_DIRNAME
	load "common"
	server_net1=$(wait_for_net1_ip "test-ingress-ns-selector-no-pods" "pod-server")
	client_a_net1=$(wait_for_net1_ip "test-ingress-ns-selector-no-pods" "pod-client-a")
	client_b_net1=$(wait_for_net1_ip "test-ingress-ns-selector-no-pods-blue" "pod-client-b")
}

teardown_file() {
	teardown_file_common
}


@test "test-ingress-ns-selector-no-pods check client-a -> server" {
	# nc should NOT succeed from client-a to server by policy
	run retry_until_deny 10 kubectl -n test-ingress-ns-selector-no-pods exec pod-client-a -- sh -c "echo x | nc -w 1 ${server_net1} 5555"
	[ "$status" -eq  "0" ]
}

@test "test-ingress-ns-selector-no-pods check client-b -> server" {
	# nc should NOT succeed from client-b to server by policy
	run retry_until_deny 10 kubectl -n test-ingress-ns-selector-no-pods-blue exec pod-client-b -- sh -c "echo x | nc -w 1 ${server_net1} 5555"
	[ "$status" -eq  "0" ]
}

@test "test-ingress-ns-selector-no-pods check server -> client-a" {
	# nc should succeed from server to client-a by no policy definition for direction (egress for pod-server)
	run kubectl -n test-ingress-ns-selector-no-pods exec pod-server -- sh -c "echo x | nc -w 1 ${client_a_net1} 5555"
	[ "$status" -eq  "0" ]
}

@test "test-ingress-ns-selector-no-pods check server -> client-b" {
	# nc should succeed from server to client-b by no policy definition for direction (egress for pod-server)
	run kubectl -n test-ingress-ns-selector-no-pods exec pod-server -- sh -c "echo x | nc -w 1 ${client_b_net1} 5555"
	[ "$status" -eq  "0" ]
}
