#!/usr/bin/env bats

# Named ports resolve against the container ports of the receiving pod: for
# ingress the selected pod (pod-a), for egress each peer. The port named "web"
# is 5555 on pod-a, 7777 on pod-b and 8888 on pod-c.

setup_file() {
	cd $BATS_TEST_DIRNAME
	load "common"
	export MANIFEST_FILE="named-ports.yml"
	kubectl apply --wait --timeout=${kubewait_timeout} -f "${MANIFEST_FILE}"
	kubectl -n test-named-ports wait --for=condition=ready -l app=test-named-ports pod --timeout=${kubewait_timeout}
	wait_for_nft_rules "test-named-ports" "pod-a" "test-multinetwork-policy-named-ports"
	wait_for_nft_rules "test-named-ports" "pod-a" "named-port-set"
}

setup() {
	cd $BATS_TEST_DIRNAME
	load "common"
	pod_a_net1=$(wait_for_net1_ip "test-named-ports" "pod-a")
	pod_b_net1=$(wait_for_net1_ip "test-named-ports" "pod-b")
	pod_c_net1=$(wait_for_net1_ip "test-named-ports" "pod-c")
}

teardown_file() {
	teardown_file_common
}

@test "test-named-ports ingress pod-b -> pod-a web (5555) OK" {
	run retry_until_success 30 kubectl -n test-named-ports exec pod-b -- sh -c "echo x | nc -w 2 ${pod_a_net1} 5555"
	[ "$status" -eq "0" ]
}

@test "test-named-ports ingress pod-b -> pod-a 6666 KO" {
	run retry_until_deny 30 kubectl -n test-named-ports exec pod-b -- sh -c "echo x | nc -w 2 ${pod_a_net1} 6666"
	[ "$status" -eq "0" ]
}

@test "test-named-ports egress pod-a -> pod-b web (7777) OK" {
	run retry_until_success 30 kubectl -n test-named-ports exec pod-a -- sh -c "echo x | nc -w 2 ${pod_b_net1} 7777"
	[ "$status" -eq "0" ]
}

@test "test-named-ports egress pod-a -> pod-c web (8888) OK" {
	run retry_until_success 30 kubectl -n test-named-ports exec pod-a -- sh -c "echo x | nc -w 2 ${pod_c_net1} 8888"
	[ "$status" -eq "0" ]
}

@test "test-named-ports pod-b -> pod-c 7777 OK (no policy on pod-c)" {
	run retry_until_success 30 kubectl -n test-named-ports exec pod-b -- sh -c "echo x | nc -w 2 ${pod_c_net1} 7777"
	[ "$status" -eq "0" ]
}

@test "test-named-ports egress pod-a -> pod-c 7777 KO (web on pod-b only)" {
	run retry_until_deny 30 kubectl -n test-named-ports exec pod-a -- sh -c "echo x | nc -w 2 ${pod_c_net1} 7777"
	[ "$status" -eq "0" ]
}

@test "test-named-ports egress pod-a -> pod-b 6666 KO" {
	run retry_until_deny 30 kubectl -n test-named-ports exec pod-a -- sh -c "echo x | nc -w 2 ${pod_b_net1} 6666"
	[ "$status" -eq "0" ]
}
