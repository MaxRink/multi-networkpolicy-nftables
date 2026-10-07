
This demo assumes both working go & kind environments. For more information on
kind check:
https://kind.sigs.k8s.io/docs/user/quick-start/

Run a kind cluster:
```
kind create cluster
```

Deploy multus:
```
kubectl apply -f https://raw.githubusercontent.com/k8snetworkplumbingwg/multus-cni/master/deployments/multus-daemonset.yml
```

Deploy the MultiNetworkPolicy CRD:
```
kubectl apply -f https://raw.githubusercontent.com/k8snetworkplumbingwg/multi-networkpolicy/master/scheme.yml
```

Deploy the multi-networkpolicy implementation with nftables. The manifests
and CNI archive below target amd64 nodes. The generated deployment defaults
to CRI-O; override its runtime endpoint for kind's containerd:
```
kubectl apply -f https://raw.githubusercontent.com/telekom/multi-networkpolicy-nftables/nftables/deploy.yml
kubectl -n kube-system patch daemonset multi-networkpolicy-ds-amd64 --type=json -p='[{"op":"replace","path":"/spec/template/spec/containers/0/args/1","value":"--container-runtime-endpoint=/run/containerd/containerd.sock"}]'
```

Copy macvlan cni to the control plane node:
```
curl -sSf -L --retry 5 https://github.com/containernetworking/plugins/releases/download/v1.5.0/cni-plugins-linux-amd64-v1.5.0.tgz | tar -xz -C . ./macvlan
...
docker cp macvlan kind-control-plane:/opt/cni/bin/
```

Deploy a sample [network attachment definition](net.yml), its
[policy](policy.yml) and [pod](alpine.yml) that attaches to that
network:
```
kubectl apply -f https://raw.githubusercontent.com/telekom/multi-networkpolicy-nftables/nftables/demo/net.yml
kubectl apply -f https://raw.githubusercontent.com/telekom/multi-networkpolicy-nftables/nftables/demo/policy.yml
kubectl apply -f https://raw.githubusercontent.com/telekom/multi-networkpolicy-nftables/nftables/demo/alpine.yml
```

You can then log in to the alpine pod and check the nftables rules enforcing
the policy. The exact rules may change as nftables rule generation evolves.

```
kubectl exec -ti alpine -- /bin/sh
...
apk update
apk add nftables
nft list ruleset
```
