#!/usr/bin/env bats

# Note:
# These test cases verify behavior when two MultiNetworkPolicies that target
# the same pod/network define different ingress rules on different ports.
# Policy 1 allows client-a on port 5555; Policy 2 allows client-b on port 6666.
# Each policy must be enforced independently: client-c is blocked on both ports.

setup() {
	cd $BATS_TEST_DIRNAME
	load "common"
	server_net1=$(get_net1_ip "test-conflicting-policies" "pod-server")
}

@test "setup conflicting-policies test environments" {
	# create test manifests
	kubectl apply --wait --timeout=${kubewait_timeout} -f conflicting-policies.yml

	# verify all pods are available
	run kubectl -n test-conflicting-policies wait --for=condition=ready -l app=test-conflicting-policies pod --timeout=${kubewait_timeout}
	[ "$status" -eq "0" ]

	# wait for nft rules from both policies to be programmed by the daemon
	wait_for_nft_rule "test-conflicting-policies" "pod-server" "test-multinetwork-policy-conflict-1"
	wait_for_nft_rule "test-conflicting-policies" "pod-server" "test-multinetwork-policy-conflict-2"
}

@test "conflicting-policies check both policy nft rules appear on pod-server" {
	# both policies must generate nft rules on pod-server
	run kubectl -n test-conflicting-policies exec pod-server -- sh -c "nft list ruleset | grep test-multinetwork-policy-conflict-1"
	[ "$status" -eq "0" ]
	run kubectl -n test-conflicting-policies exec pod-server -- sh -c "nft list ruleset | grep test-multinetwork-policy-conflict-2"
	[ "$status" -eq "0" ]
	# client pods must NOT have rules (policies target pod-server only)
	run kubectl -n test-conflicting-policies exec pod-client-a -- sh -c "nft list ruleset | grep test-multinetwork-policy-conflict-1"
	[ "$status" -eq "1" ]
	run kubectl -n test-conflicting-policies exec pod-client-b -- sh -c "nft list ruleset | grep test-multinetwork-policy-conflict-2"
	[ "$status" -eq "1" ]
}

@test "conflicting-policies client-a -> server:5555 allowed (policy 1)" {
	# policy 1 allows client-a on port 5555
	retry_until_success 10 kubectl -n test-conflicting-policies exec pod-client-a -- sh -c "echo x | nc -w 1 ${server_net1} 5555"
}

@test "conflicting-policies client-a -> server:6666 blocked (policy 2 allows client-b only)" {
	# client-a is not listed in policy 2, so port 6666 must be blocked for client-a
	run retry_until_deny 10 kubectl -n test-conflicting-policies exec pod-client-a -- sh -c "echo x | nc -w 1 ${server_net1} 6666"
	[ "$status" -eq 0 ]
}

@test "conflicting-policies client-b -> server:6666 allowed (policy 2)" {
	# policy 2 allows client-b on port 6666
	run kubectl -n test-conflicting-policies exec pod-client-b -- sh -c "echo x | nc -w 1 ${server_net1} 6666"
	[ "$status" -eq "0" ]
}

@test "conflicting-policies client-b -> server:5555 blocked (policy 1 allows client-a only)" {
	# client-b is not listed in policy 1, so port 5555 must be blocked for client-b
	run retry_until_deny 10 kubectl -n test-conflicting-policies exec pod-client-b -- sh -c "echo x | nc -w 1 ${server_net1} 5555"
	[ "$status" -eq 0 ]
}

@test "conflicting-policies client-c -> server:5555 blocked (no policy allows client-c)" {
	# client-c has no matching policy rule on any port
	run retry_until_deny 10 kubectl -n test-conflicting-policies exec pod-client-c -- sh -c "echo x | nc -w 1 ${server_net1} 5555"
	[ "$status" -eq 0 ]
}

@test "conflicting-policies client-c -> server:6666 blocked (no policy allows client-c)" {
	# client-c has no matching policy rule on any port
	run retry_until_deny 10 kubectl -n test-conflicting-policies exec pod-client-c -- sh -c "echo x | nc -w 1 ${server_net1} 6666"
	[ "$status" -eq 0 ]
}

teardown_file() {
	cd $BATS_TEST_DIRNAME
	kubectl delete -f conflicting-policies.yml --ignore-not-found
	kubectl -n test-conflicting-policies wait --for=delete -l app=test-conflicting-policies pod --timeout=${kubewait_timeout} 2>/dev/null || true
}
