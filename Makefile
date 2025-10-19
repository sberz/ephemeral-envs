## help: print this help message
.PHONY: help
help:
	@echo 'Usage:'
	@sed -n 's/^##//p' ${MAKEFILE_LIST} | column -t -s ':' |  sed -e 's/^/ /'

## build: build the project
.PHONY: build
build:
	go build -o bin/autodiscovery ./cmd/autodiscovery

## lint: lint the codebase
.PHONY: lint
lint:
	@echo "Running linters..."
	golangci-lint run ./...

## testing/setup/cluster: setup kind cluster for testing
.PHONY: testing/setup/cluster
testing/setup/cluster:
	@./scripts/test-cluster.sh setup-minimal

## testing/setup: setup testing environment
.PHONY: testing/setup
testing/setup:
	@./scripts/test-cluster.sh setup-cluster

## testing/load-image: Build and load Docker image into kind cluster
.PHONY: testing/load-image
testing/load-image:
	@./scripts/test-cluster.sh load-image

## testing/install-helm: Install Helm chart into kind cluster
.PHONY: testing/install-helm
testing/install-helm:
	@./scripts/test-cluster.sh install-helm

## testing/examples: Apply example manifests for testing
.PHONY: testing/examples
testing/examples:
	@./scripts/test-cluster.sh examples

## testing/teardown: teardown testing environment
.PHONY: testing/teardown
testing/teardown:
	@./scripts/test-cluster.sh teardown

