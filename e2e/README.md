## e2e test with kind


### How to test e2e

This requires [Bats](https://github.com/bats-core/bats-core) for test runner. Please install bats (e.g. dnf, apt and so on).
Use a disposable Linux/amd64 kind environment; the current installation manifests
are amd64-only.

```
$ git clone --branch nftables https://github.com/telekom/multi-networkpolicy-nftables
$ cd multi-networkpolicy-nftables/e2e
$ ./get_tools.sh
$ ./setup_cluster.sh
$ ./run_all_tests.sh
```

### How to teardown cluster

```
$ ./bin/kind delete cluster
```

### How to deploy server image with new changes

After making changes to the code, recreate the disposable test cluster.
`setup_cluster.sh` rebuilds and loads the local images before installing the
DaemonSet:

```
./bin/kind delete cluster
./setup_cluster.sh
./run_all_tests.sh
```
