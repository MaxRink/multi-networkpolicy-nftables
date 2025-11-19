# This Dockerfile is used to build the image available on DockerHub
FROM golang:1.21 AS build

# Add everything
ADD . /usr/src/multi-networkpolicy-nftables

RUN cd /usr/src/multi-networkpolicy-nftables && \
    CGO_ENABLED=0 go build ./cmd/multi-networkpolicy-nftables/

FROM docker.io/debian:stable-slim
LABEL org.opencontainers.image.source=https://github.com/telekom/multi-networkpolicy-nftables
RUN apt update \
    && apt install -y --no-install-recommends \
    nftables \
    && apt clean \
    && rm -Rf /usr/share/doc && rm -Rf /usr/share/man \
    && rm -rf /var/lib/apt/lists/* \
    && touch -d "2 hours ago" /var/lib/apt/lists
COPY --from=build /usr/src/multi-networkpolicy-nftables/multi-networkpolicy-nftables /usr/bin
WORKDIR /usr/bin

ENTRYPOINT ["multi-networkpolicy-nftables"]
