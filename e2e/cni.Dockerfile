FROM docker.io/debian:stable-slim

RUN apt update \
    && apt install -y --no-install-recommends \
    wget \
    ca-certificates \
    && apt clean \
    && rm -Rf /usr/share/doc && rm -Rf /usr/share/man \
    && rm -rf /var/lib/apt/lists/* \
    && touch -d "2 hours ago" /var/lib/apt/lists

RUN wget https://github.com/containernetworking/plugins/releases/download/v1.8.0/cni-plugins-linux-amd64-v1.8.0.tgz -O /opt/cni-plugins.tgz
