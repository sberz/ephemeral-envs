
KIND_CLUSTER_NAME ?= test-ephemeral-envs
KIND_KUBECONFIG ?= $(abspath ./kind-kubeconfig.yaml)

export KUBECONFIG=$(KIND_KUBECONFIG)


## help: print this help message
.PHONY: help
help:
	@echo 'Usage:'
	@sed -n 's/^##//p' ${MAKEFILE_LIST} | column -t -s ':' |  sed -e 's/^/ /'


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

## testing/teardown: teardown testing environment
.PHONY: testing/teardown
testing/teardown:
	@echo "Tearing down testing environment..."
	kind delete cluster --name test-ephemeral-envs
	@echo "Cluster deleted."


