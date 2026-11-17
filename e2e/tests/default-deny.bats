#!/usr/bin/env bats

# Note:
# These test cases switch the daemon to --default-deny=ingress and check that a
# pod no policy selects becomes unreachable on its secondary network, that a
# policy re-opens exactly the port it allows, and that --default-deny=none
# (the default) leaves the pod reachable.

ds=multi-networkpolicy-ds-amd64

# set_default_deny replaces any --default-deny argument of the DaemonSet with
# --default-deny=<mode> and waits for the rollout.
set_default_deny() {
	local args
	args=$(kubectl -n kube-system get daemonset "$ds" -o json | jq -c --arg flag "--default-deny=$1" \
		'[.spec.template.spec.containers[0].args[] | select(startswith("--default-deny=") | not)] + [$flag]')
	kubectl -n kube-system patch daemonset "$ds" --type json \
		-p "[{\"op\": \"replace\", \"path\": \"/spec/template/spec/containers/0/args\", \"value\": ${args}}]"
	kubectl -n kube-system rollout status daemonset/"$ds" --timeout=${kubewait_timeout}
	kubectl -n kube-system wait --for=condition=ready -l app=multi-networkpolicy pod --timeout=${kubewait_timeout}
}

setup_file() {
	cd $BATS_TEST_DIRNAME
	load "common"
	export MANIFEST_FILE="default-deny.yml"
	ensure_daemonset_running
	kubectl apply --wait --timeout=${kubewait_timeout} -f "${MANIFEST_FILE}"
	kubectl -n test-default-deny wait --for=condition=ready -l app=test-default-deny pod --timeout=${kubewait_timeout}
}

setup() {
	cd $BATS_TEST_DIRNAME
	load "common"
	server_net1=$(wait_for_net1_ip "test-default-deny" "pod-server")
}

teardown_file() {
	cd $BATS_TEST_DIRNAME
	load "common"
	set_default_deny none
	teardown_file_common
}

@test "default-deny none: pod without policy is reachable" {
	run retry_until_success 10 kubectl -n test-default-deny exec pod-client -- sh -c "echo x | nc -w 1 ${server_net1} 5555"
	[ "$status" -eq "0" ]
	run kubectl -n test-default-deny exec pod-server -- sh -c "nft list ruleset | grep drop-remaining"
	[ "$status" -eq "1" ]
}

@test "default-deny ingress: pod without policy is unreachable" {
	set_default_deny ingress
	run wait_for_nft_rules test-default-deny pod-server drop-remaining
	[ "$status" -eq "0" ]
	run retry_until_deny 30 kubectl -n test-default-deny exec pod-client -- sh -c "echo x | nc -w 1 ${server_net1} 5555"
	[ "$status" -eq "0" ]
	run retry_until_deny 30 kubectl -n test-default-deny exec pod-client -- sh -c "echo x | nc -w 1 ${server_net1} 6666"
	[ "$status" -eq "0" ]
	# The cluster network (eth0) is out of scope and stays reachable.
	server_eth0=$(kubectl -n test-default-deny get pod pod-server -o jsonpath='{.status.podIP}')
	run retry_until_allow 30 kubectl -n test-default-deny exec pod-client -- sh -c "echo x | nc -w 1 ${server_eth0} 6666"
	[ "$status" -eq "0" ]
}

@test "default-deny ingress: a policy re-opens the port it allows" {
	kubectl apply -f default-deny-policy.yml
	run wait_for_nft_rules test-default-deny pod-server test-default-deny-allow-5555
	[ "$status" -eq "0" ]
	# The reply reaches pod-client, whose ingress is denied too, as
	# established traffic.
	run retry_until_allow 30 kubectl -n test-default-deny exec pod-client -- sh -c "echo x | nc -w 1 ${server_net1} 5555"
	[ "$status" -eq "0" ]
	run retry_until_deny 30 kubectl -n test-default-deny exec pod-client -- sh -c "echo x | nc -w 1 ${server_net1} 6666"
	[ "$status" -eq "0" ]
}

@test "default-deny none again: drop rule is removed" {
	kubectl delete -f default-deny-policy.yml
	set_default_deny none
	run wait_for_nft_rule_absent test-default-deny pod-server drop-remaining
	[ "$status" -eq "0" ]
	run retry_until_allow 30 kubectl -n test-default-deny exec pod-client -- sh -c "echo x | nc -w 1 ${server_net1} 6666"
	[ "$status" -eq "0" ]
}
