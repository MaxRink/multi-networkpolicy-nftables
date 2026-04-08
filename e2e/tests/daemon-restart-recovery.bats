#!/usr/bin/env bats

# Note:
# These test cases verify that after a kubectl rollout restart of the
# multi-networkpolicy DaemonSet, nft rules are correctly re-applied to
# pods. This ensures the daemon recovers from restarts without leaving
# pods unprotected.

setup_file() {
	cd $BATS_TEST_DIRNAME
	load "common"
	export MANIFEST_FILE="daemon-restart-recovery.yml"

	# create test manifests
	kubectl apply --wait --timeout=${kubewait_timeout} -f "${MANIFEST_FILE}"

	# verify all pods are available
	kubectl -n test-daemon-restart wait --for=condition=ready -l app=test-daemon-restart pod --timeout=${kubewait_timeout}

	# wait for nft rules to be programmed by the daemon
	wait_for_nft_rule "test-daemon-restart" "pod-server" "test-multinetwork-policy-daemon-restart-1"

	# wait for policy enforcement to be active: client-b must be blocked before proceeding
	local server_net1
	server_net1=$(get_net1_ip "test-daemon-restart" "pod-server")
	wait_for_connectivity_blocked "test-daemon-restart" "pod-client-b" "$server_net1" "5555"
}

setup() {
	cd $BATS_TEST_DIRNAME
	load "common"
	server_net1=$(wait_for_net1_ip "test-daemon-restart" "pod-server")
}

teardown_file() {
	teardown_file_common
}

@test "daemon-restart check nft rules exist before restart" {
	# pod-server should have nft rules for the ingress policy
	run kubectl -n test-daemon-restart exec pod-server -- sh -c "nft list ruleset | grep test-multinetwork-policy-daemon-restart-1"
	[ "$status" -eq "0" ]
	# client pods should NOT have rules (policy targets pod-server only)
	run kubectl -n test-daemon-restart exec pod-client-a -- sh -c "nft list ruleset | grep test-multinetwork-policy-daemon-restart-1"
	[ "$status" -eq "1" ]
	run kubectl -n test-daemon-restart exec pod-client-b -- sh -c "nft list ruleset | grep test-multinetwork-policy-daemon-restart-1"
	[ "$status" -eq "1" ]
}

@test "daemon-restart check client-a -> server allowed before restart" {
	# nc should succeed from client-a to server (allowed by policy)
	run kubectl -n test-daemon-restart exec pod-client-a -- sh -c "echo x | nc -w 1 ${server_net1} 5555"
	[ "$status" -eq "0" ]
}

@test "daemon-restart check client-b -> server blocked before restart" {
	# nc should NOT succeed from client-b to server (not allowed by policy)
	run retry_until_deny 30 kubectl -n test-daemon-restart exec pod-client-b -- sh -c "echo x | nc -w 1 ${server_net1} 5555"
	[ "$status" -eq 0 ]
}

@test "daemon-restart restart DaemonSet and verify nft rules re-applied" {
	# restart the multi-networkpolicy DaemonSet
	kubectl get daemonset -n kube-system -l app=multi-networkpolicy -o name | xargs -I{} kubectl rollout restart -n kube-system {}

	# wait for rollout to complete
	kubectl get daemonset -n kube-system -l app=multi-networkpolicy -o name | xargs -I{} kubectl rollout status -n kube-system {} --timeout=${kubewait_timeout}

	# wait for daemon to re-sync nft rules to all pods
	wait_for_nft_rule "test-daemon-restart" "pod-server" "test-multinetwork-policy-daemon-restart-1"

	# wait for policy enforcement to be active: client-b must be blocked before proceeding
	wait_for_connectivity_blocked "test-daemon-restart" "pod-client-b" "$server_net1" "5555"

	# pod-server must have nft rules re-applied after daemon restart
	run kubectl -n test-daemon-restart exec pod-server -- sh -c "nft list ruleset | grep test-multinetwork-policy-daemon-restart-1"
	[ "$status" -eq "0" ]
}

@test "daemon-restart check client-a -> server still allowed after restart" {
	# policy enforcement must survive daemon restart
	run kubectl -n test-daemon-restart exec pod-client-a -- sh -c "echo x | nc -w 1 ${server_net1} 5555"
	[ "$status" -eq "0" ]
}

@test "daemon-restart check client-b -> server still blocked after restart" {
	# blocks must remain in place after daemon restart
	run retry_until_deny 30 kubectl -n test-daemon-restart exec pod-client-b -- sh -c "echo x | nc -w 1 ${server_net1} 5555"
	[ "$status" -eq 0 ]
}
