
KIND_CLUSTER_NAME ?= test-ephemeral-envs
KIND_KUBECONFIG ?= $(abspath ./kind-kubeconfig.yaml)

IMAGE_NAME ?= ghcr.io/sberz/ephemeral-envs
IMAGE_TAG ?= local

export KIND_EXPERIMENTAL_PROVIDER ?= podman
export KUBECONFIG=$(KIND_KUBECONFIG)


## help: print this help message
.PHONY: help
help:
	@echo 'Usage:'
	@sed -n 's/^##//p' ${MAKEFILE_LIST} | column -t -s ':' |  sed -e 's/^/ /'

## buld: build the project
.PHONY: build
build:
	go build -o bin/autodiscovery ./cmd/autodiscovery

## build-image: build the container image
.PHONY: build-image
build-image:
	podman build -t $(IMAGE_NAME):$(IMAGE_TAG) .

## lint: lint the codebase
.PHONY: lint
lint:
	@echo "Running linters..."
	golangci-lint run ./...

## testing/setup: setup testing environment
.PHONY: testing/setup
testing/setup:
	@echo "Setting up testing environment..."
	@kind create cluster --name test-ephemeral-envs
	@echo "Cluster created. Kubeconfig is available at $(KIND_KUBECONFIG)."

## testing/get-env: export environment variables for testing
.PHONY: testing/get-env
testing/get-env:
	@echo "Exporting environment variables for testing..." >&2
	@echo "# HINT: Runt this command to set the environment variables in your shell:"
	@echo "# source <(make testing/get-env)\n"

	@echo "export KUBECONFIG=$(KIND_KUBECONFIG)"

## testing/load-image: Build and load Docker image into kind cluster
.PHONY: testing/load-image
testing/load-image: build-image
	@echo "Loading image $(IMAGE_NAME):$(IMAGE_TAG) into kind cluster..."
	kind load docker-image $(IMAGE_NAME):$(IMAGE_TAG) --name $(KIND_CLUSTER_NAME)

## testing/teardown: teardown testing environment
.PHONY: testing/teardown
testing/teardown:
	@echo "Tearing down testing environment..."
	kind delete cluster --name test-ephemeral-envs
	@echo "Cluster deleted."


