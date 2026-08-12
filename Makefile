# Multi-NetworkPolicy nftables - Makefile
BINARY_NAME ?= multi-networkpolicy-nftables
CMD_DIR ?= ./cmd/multi-networkpolicy-nftables/
GOARCH ?= $(shell go env GOARCH)
GOOS ?= $(shell go env GOOS)
HOST_OS ?= $(shell uname -s | tr '[:upper:]' '[:lower:]')
GO_LDFLAGS ?= -s -w
IMAGE_REPO ?= ghcr.io/telekom/multi-networkpolicy-nftables
IMAGE_TAG ?= dev
GOVULNCHECK_VERSION ?= v1.1.4
GOVULNCHECK ?= $(shell go env GOPATH)/bin/govulncheck
TEST_PROFILE ?= profile.cov
TEST_ALL_PKGS ?= ./...
TEST_UNPRIVILEGED_PKGS ?= ./pkg/controller ./pkg/controllers ./pkg/utils
TEST_NFTABLES_PKGS ?= ./pkg/server

.PHONY: all build test lint vet govulncheck fmt fmt-fix clean e2e image manifests verify-manifests help

all: build

## build: Build the binary
build:
	CGO_ENABLED=0 GOARCH=$(GOARCH) GOOS=$(GOOS) go build -ldflags "$(GO_LDFLAGS)" -o $(BINARY_NAME)_$(GOOS)_$(GOARCH) $(CMD_DIR)

## test: Run all unit tests (requires Linux and root/passwordless sudo for nftables tests)
test: TEST_NFTABLES_PKGS = $(TEST_ALL_PKGS)
test: test-nftables

## test-unprivileged: Run unit tests that do not need Linux nftables/root
test-unprivileged:
	@set -e; \
	if [ -n "$${KUBEBUILDER_ASSETS:-}" ]; then \
		KUBEBUILDER_ASSETS=$$(cd "$$KUBEBUILDER_ASSETS" && pwd); \
		export KUBEBUILDER_ASSETS; \
	fi; \
	go test -v $(TEST_UNPRIVILEGED_PKGS)

## test-nftables: Run nftables-backed unit tests (requires Linux and root/passwordless sudo)
test-nftables:
	@set -e; \
	if [ -n "$${KUBEBUILDER_ASSETS:-}" ]; then \
		KUBEBUILDER_ASSETS=$$(cd "$$KUBEBUILDER_ASSETS" && pwd); \
		export KUBEBUILDER_ASSETS; \
	fi; \
	if [ "$(HOST_OS)" != "linux" ]; then \
		echo "make $(if $(MAKECMDGOALS),$(firstword $(MAKECMDGOALS)),$@) requires Linux for nftables tests"; \
		exit 1; \
	elif [ "$$(id -u)" -eq 0 ]; then \
		modprobe nft_ct 2>/dev/null || true; \
		uid=$$(id -u); gid=$$(id -g); \
		go test -v -coverprofile="$(TEST_PROFILE)" $(TEST_NFTABLES_PKGS); \
	elif command -v sudo >/dev/null 2>&1 && sudo -n true >/dev/null 2>&1; then \
		sudo -n modprobe nft_ct 2>/dev/null || true; \
		uid=$$(id -u); gid=$$(id -g); \
		status=0; \
		if [ -n "$${KUBEBUILDER_ASSETS:-}" ]; then \
			sudo env "KUBEBUILDER_ASSETS=$$KUBEBUILDER_ASSETS" go test -v -coverprofile="$(TEST_PROFILE)" $(TEST_NFTABLES_PKGS) || status=$$?; \
		else \
			sudo go test -v -coverprofile="$(TEST_PROFILE)" $(TEST_NFTABLES_PKGS) || status=$$?; \
		fi; \
		if [ -f "$(TEST_PROFILE)" ]; then sudo chown "$$uid:$$gid" "$(TEST_PROFILE)"; fi; \
		exit $$status; \
	else \
		echo "make $(if $(MAKECMDGOALS),$(firstword $(MAKECMDGOALS)),$@) requires root or passwordless sudo; refusing to prompt interactively"; \
		exit 1; \
	fi

## lint: Run golangci-lint
lint:
	golangci-lint run

## vet: Run go vet
vet:
	go vet ./...

## govulncheck: Run Go vulnerability analysis
govulncheck:
	go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	$(GOVULNCHECK) ./...

## fmt: Check formatting
fmt:
	@diff=$$(gofmt -d -s ./cmd/ ./pkg/); \
	if [ -n "$$diff" ]; then \
		echo "$$diff"; \
		exit 1; \
	fi

## fmt-fix: Fix formatting
fmt-fix:
	gofmt -w -s ./cmd/ ./pkg/

## clean: Remove build artifacts
clean:
	rm -f $(BINARY_NAME)_* "$(TEST_PROFILE)"

## image: Build container image
image:
	docker build -t $(IMAGE_REPO):$(IMAGE_TAG) -f Dockerfile .

## e2e: Run e2e tests (requires kind cluster)
e2e:
	cd e2e && ./run_all_tests.sh

## manifests: Regenerate deploy.yml and e2e install manifests
manifests:
	./hack/update-deploy-manifests.sh

## verify-manifests: Verify generated manifests are up to date
verify-manifests:
	./hack/verify-deploy-manifests.sh

## help: Show this help
help:
	@echo "Available targets:"
	@awk '/^## / { line = substr($$0, 4); split(line, parts, ": "); printf "  %s\t%s\n", parts[1], parts[2] }' Makefile
