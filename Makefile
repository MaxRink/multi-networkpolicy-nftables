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
GOVULNCHECK_ALLOWED ?= GO-2025-3547 GO-2025-3521

.PHONY: all build test lint vet fmt fmt-fix clean e2e image manifests verify-manifests help

all: build

## build: Build the binary
build:
	CGO_ENABLED=0 GOARCH=$(GOARCH) GOOS=$(GOOS) go build -ldflags "$(GO_LDFLAGS)" -o $(BINARY_NAME)_$(GOOS)_$(GOARCH) $(CMD_DIR)

## test: Run unit tests (requires root for nftables tests)
test:
	@if [ "$(HOST_OS)" != "linux" ]; then \
		echo "make test requires Linux for nftables tests"; \
		exit 1; \
	elif [ "$$(id -u)" -eq 0 ]; then \
		modprobe nft_ct 2>/dev/null || true; \
		uid=$$(id -u); gid=$$(id -g); \
		go test -v -coverprofile=profile.cov ./...; \
	elif command -v sudo >/dev/null 2>&1 && sudo -n true >/dev/null 2>&1; then \
		sudo -n modprobe nft_ct 2>/dev/null || true; \
		uid=$$(id -u); gid=$$(id -g); \
		status=0; \
		if [ -n "$${KUBEBUILDER_ASSETS:-}" ]; then \
			KUBEBUILDER_ASSETS=$$(cd "$$KUBEBUILDER_ASSETS" && pwd); \
			sudo env "KUBEBUILDER_ASSETS=$$KUBEBUILDER_ASSETS" go test -v -coverprofile=profile.cov ./... || status=$$?; \
		else \
			sudo go test -v -coverprofile=profile.cov ./... || status=$$?; \
		fi; \
		if [ -f profile.cov ]; then sudo chown "$$uid:$$gid" profile.cov; fi; \
		exit $$status; \
	else \
		echo "make test requires root or passwordless sudo; refusing to prompt interactively"; \
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
	@tmp=$$(mktemp); \
	status=0; \
	$(GOVULNCHECK) ./... > "$$tmp" 2>&1 || status=$$?; \
	cat "$$tmp"; \
	if [ "$$status" -eq 0 ]; then \
		rm -f "$$tmp"; \
		exit 0; \
	fi; \
	ids=$$(grep -Eo 'GO-[0-9]{4}-[0-9]+' "$$tmp" | sort -u || true); \
	unexpected=""; \
	for id in $$ids; do \
		case " $(GOVULNCHECK_ALLOWED) " in \
			*" $$id "*) ;; \
			*) unexpected="$$unexpected $$id" ;; \
		esac; \
	done; \
	rm -f "$$tmp"; \
	if [ -z "$$ids" ] || [ -n "$$unexpected" ]; then \
		echo "govulncheck failed with unexpected findings:$${unexpected:- none parsed}"; \
		exit "$$status"; \
	fi; \
	echo "govulncheck found only allowed upstream findings: $$ids"

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
	rm -f $(BINARY_NAME)_* profile.cov

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
