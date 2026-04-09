#!/usr/bin/env bats

# Note:
# These test cases, simple, will create simple (one policy for ingress) and test the 
# traffic policying by ncat (nc) command. In addition, these cases also verifies that
# simple nftables generation check pod-iptable in multi-networkpolicy pod.

setup_file() {
	cd $BATS_TEST_DIRNAME
	load "common"
	export MANIFEST_FILE="protocol-only-ports.yml"
	kubectl apply --wait --timeout=${kubewait_timeout} -f "${MANIFEST_FILE}"
	kubectl -n test-protocol-only-ports wait --for=condition=ready -l app=test-protocol-only-ports pod --timeout=${kubewait_timeout}
	wait_for_nft_rules "test-protocol-only-ports" "pod-a" "test-multinetwork-policy-simple-1"
}

setup() {
	cd $BATS_TEST_DIRNAME
	load "common"
	pod_a_net1=$(wait_for_net1_ip "test-protocol-only-ports" "pod-a")
	pod_b_net1=$(wait_for_net1_ip "test-protocol-only-ports" "pod-b")
}

teardown_file() {
	teardown_file_common
}


@test "test-protocol-only-ports check pod-a -> pod-b TCP" {
	# nc should succeed from client-a to server by policy
	run kubectl -n test-protocol-only-ports exec pod-a -- sh -c "echo x | nc -w 2 ${pod_b_net1} 5555"
	[ "$status" -eq  "0" ]
}

@test "test-protocol-only-ports check pod-a -> pod-b UDP" {
	# nc should be denied from pod-a to pod-b UDP by policy
	run retry_until_deny 30 kubectl -n test-protocol-only-ports exec pod-a -- sh -c "echo x | nc --udp -w 2 ${pod_b_net1} 6666"
	[ "$status" -eq  "0" ]
}

@test "test-protocol-only-ports check pod-b -> pod-a TCP" {
	# nc should be denied from pod-b to pod-a TCP by policy
	run retry_until_deny 30 kubectl -n test-protocol-only-ports exec pod-b -- sh -c "echo x | nc -w 2 ${pod_a_net1} 5555"
	[ "$status" -eq  "0" ]
}

@test "test-protocol-only-ports check pod-b -> pod-a UDP" {
	# nc should succeed from client-a to server by policy
	run kubectl -n test-protocol-only-ports exec pod-b -- sh -c "echo x | nc --udp -w 2 ${pod_a_net1} 6666"
	[ "$status" -eq  "0" ]
}


