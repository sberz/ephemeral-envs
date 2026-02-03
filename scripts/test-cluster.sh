#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"/..

: "${KIND_CLUSTER_NAME:=test-ephemeral-envs}"
: "${KIND_KUBECONFIG:="$(realpath ./kind-kubeconfig.yaml)"}"
: "${INGRESS_PORT:=3000}"

: "${IMAGE_NAME:=ghcr.io/sberz/ephemeral-envs}"
: "${IMAGE_TAG:=local}"


export KUBECONFIG="${KIND_KUBECONFIG}"

export KIND_EXPERIMENTAL_PROVIDER="podman"

log_info() {
	echo -e "\033[1;34m[INFO]\033[0m $1"
}

log_fatal() {
	echo -e "\033[1;31m[FATAL]\033[0m $1"
	exit 1
}

print_usage() {
	echo "Usage: $0 <command>"
	echo "Commands:"
	echo "  help           Show this help message."
	echo "  examples       Apply example manifests to the cluster."
	echo "  load-image     Build and load container image into the kind cluster."
	echo "  install-helm   Install the Helm chart into the kind cluster."
	echo "  setup-cluster  Set up a kind cluster with necessary components for testing."
	echo "  setup-minimal  Set up a minimal kind cluster without additional components."
	echo "  teardown       Tear down the kind cluster."
}

# Check for required tools: podman, kind, kubectl, and helm
check_dependencies() {
	local dependencies=("podman" "kind" "kubectl" "helm")
	for dep in "${dependencies[@]}"; do
		if ! command -v "$dep" &> /dev/null; then
			log_fatal "Required dependency '$dep' is not installed. Please install it and try again."
		fi
	done
}

check_cluster_exists() {
	if kind get clusters | grep -q "^${KIND_CLUSTER_NAME}$"; then
		log_info "Cluster ${KIND_CLUSTER_NAME} already exists."
		return 0
	else
		log_info "Cluster ${KIND_CLUSTER_NAME} does not exist."
		return 1
	fi
}

setup_cluster() {
	log_info "Setting up kind cluster for testing..."
	cat <<-EOF | kind create cluster --name "${KIND_CLUSTER_NAME}" --kubeconfig "${KIND_KUBECONFIG}" --config=-
		kind: Cluster
		apiVersion: kind.x-k8s.io/v1alpha4
		nodes:
		  - role: control-plane
		    extraPortMappings:
		      - containerPort: 31080
		        hostPort: ${INGRESS_PORT}
		EOF

	log_info "Cluster created. Kubeconfig is available at ${KIND_KUBECONFIG}."
}

teardown_cluster() {
	log_info "Tearing down kind cluster ${KIND_CLUSTER_NAME}..."
	kind delete cluster --name "${KIND_CLUSTER_NAME}"
	log_info "Cluster ${KIND_CLUSTER_NAME} has been deleted."
}

install_gateway() {
	log_info "Installing Gateway API CRDs..."

	kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.4.0/standard-install.yaml

	log_info "Gateway API CRDs installed."
	log_info "Installing Traefik Gateway Controller..."

	helm upgrade --install traefik \
		traefik --repo https://helm.traefik.io/traefik \
		--namespace traefik --create-namespace --wait \
		--values=- <<-EOF
			ingressRoute:
			  dashboard:
			    enabled: true
			    matchRule: Host("traefik.env-test.localhost")
			    entryPoints:
			      - web
			providers:
			  kubernetesGateway:
			    enabled: true
			gateway:
			  listeners:
			    web:
			      namespacePolicy:
			        from: All
			ports:
			  web:
			    nodePort: 31080
			service:
			  type: NodePort
			EOF

	log_info "Traefik Gateway Controller installed."
	log_info "Traefik Dashboard available at http://traefik.env-test.localhost:${INGRESS_PORT}"
}

install_ingress_controller() {
	log_info "Installing NGINX Ingress Controller..."

	helm upgrade --install ingress-nginx \
		ingress-nginx --repo https://kubernetes.github.io/ingress-nginx \
		--namespace ingress-nginx --create-namespace --wait \
		--set controller.service.type=NodePort \
		--set controller.service.nodePorts.http=31080

	log_info "NGINX Ingress Controller installed."
}

install_prometheus() {
	log_info "Installing Prometheus..."

	helm upgrade --install kube-prometheus-stack \
		oci://ghcr.io/prometheus-community/charts/kube-prometheus-stack \
		--namespace monitoring --create-namespace --wait \
		--values=- <<-EOF
			prometheus:
			  prometheusSpec:
			    serviceMonitorSelectorNilUsesHelmValues: false
			    enableFeatures:
			      - native-histograms
			  route:
			    main:
			      enabled: true
			      parentRefs:
			        - name: traefik-gateway
			          namespace: traefik
			      hostnames:
			        - prometheus.env-test.localhost
			grafana:
			  route:
			    main:
			      enabled: true
			      parentRefs:
			        - name: traefik-gateway
			          namespace: traefik
			      hostnames:
			        - grafana.env-test.localhost
			EOF

	log_info "Prometheus installed."
	log_info "Prometheus available at http://prometheus.env-test.localhost:${INGRESS_PORT}"
	log_info "Grafana available at http://grafana.env-test.localhost:${INGRESS_PORT}"

	local admin_password
	admin_password=$(kubectl get secret --namespace monitoring kube-prometheus-stack-grafana -o jsonpath="{.data.admin-password}" | base64 --decode)
	log_info "Default Grafana credentials: admin / $admin_password"
}

examples_apply() {
	log_info "Applying example manifests..."
	for path in ./examples/*/manifests; do
		log_info "Applying manifests from $path"
		kubectl apply -f "$path"
	done

	log_info "Example manifests applied."
}

load_image() {
	log_info "Building container image..."
	podman build -t "${IMAGE_NAME}:${IMAGE_TAG}" .
	log_info "Container image built: ${IMAGE_NAME}:${IMAGE_TAG}"
	IMAGE_DIGEST=$(podman image inspect --format '{{.Digest}}' "${IMAGE_NAME}:${IMAGE_TAG}")

	log_info "Loading image into kind cluster ${KIND_CLUSTER_NAME}..."
	kind load docker-image "${IMAGE_NAME}:${IMAGE_TAG}" --name "${KIND_CLUSTER_NAME}"
	log_info "Image loaded into kind cluster."
}

install_helm() {
	log_info "Installing Helm chart into kind cluster..."

	# Install the Helm chart with the built image. Annotate pods with image digest
	# to enforce a restart if the image changes. Loading the image with digest and
	# using the imageDigest did not work as expected.
	helm upgrade --install ephemeral-envs ./charts/ephemeral-envs \
		--wait --values ./scripts/local-helm-values.yaml \
		--set podAnnotations.image-digest="${IMAGE_DIGEST}"

	log_info "Helm chart installed."
	log_info "Ephemeral Envs available at http://ephemeral-envs.env-test.localhost:${INGRESS_PORT}/v1/environment"
}

check_dependencies

cmd=${1:-}
case $cmd in
	help)
		print_usage
		;;
	examples)
		examples_apply
		;;
	install-helm)
		load_image
		install_helm
		;;
	load-image)
		load_image
		;;
	setup-cluster)
		check_cluster_exists || setup_cluster
		install_gateway
		install_prometheus
		;;
	setup-minimal)
		check_cluster_exists || setup_cluster
		;;
	teardown)
		teardown_cluster
		;;
	*)
		print_usage
		log_fatal "Unknown command: $cmd"
		;;
esac
