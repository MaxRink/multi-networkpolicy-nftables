FROM docker.io/fedora:38

LABEL org.opencontainers.image.source=https://github.com/k8snetworkplumbingwg/multi-networkpolicy-nftables
LABEL org.opencontainers.image.base.name=localhost:5000/multi-networkpolicy-nftables:e2e-test
RUN dnf install -y nginx net-tools iputils iproute tcpdump wireshark-cli nmap-ncat nftables
