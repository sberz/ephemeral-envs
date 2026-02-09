# ephemeral-envs

ephemeral-envs is a small service to discover ephemeral development and testing environments running in a Kubernetes cluster.
It provides a REST API to list and get details about these environments, which can be used by other tools or services.

Many published methods and tools to create ephemeral environments (e.g automatically create a environment for QA for each pull request and replying with the URL in the PR) work well for webapps. There it is really easy to use the environment URL to access the environment. But other clients (e.g. mobile apps) are usually not aware of all existing ephemeral environments and switching between them is often cumbersome or not possible without rebuilding the app.
This service can be used to discover all ephemeral environments in a cluster and provide the necessary connection details to access them and switch between environments.

## Installation

The simplest way to get started is to use the provided Helm chart available in the OCI registry
You can install it with the following command:

```bash
helm install ephemeral-envs oci://ghcr.io/sberz/charts/ephemeral-envs
```

You can find all configuration options in the [values.yaml](./charts/ephemeral-envs/values.yaml) file.

## Usage

This service makes a few assumptions about how ephemeral environments are defined in the cluster.

- Each ephemeral environment is represented by a Kubernetes namespace.
- Each namespace has a label `envs.sberz.de/name` with the name of the environment.
- Environment names must be unique across all namespaces.
- Additional information about the environment can be provided via annotations on the namespace or dynamic metadata queries.

### Accessing the Service

Once the service is running, you can access the REST API to list and get details about the ephemeral environments.

The API endpoints are:

- `GET /v1/environment`: List all ephemeral environment names.
    - Optional query parameters:
        - `namespace`: Filter by namespace.
        - `status`: Filter by status of status checks (e.g. `status=healthy`). Can be negated with `status=!healthy`. Multiple status checks can be combined with commas (e.g. `status=active,!healthy`).

- `GET /v1/environment/{name}`: Get details about a specific ephemeral environment.
- `GET /v1/environment/all`: Get details about all ephemeral environments.
	- Optional query parameters:
		- `withStatus`: Comma-separated list of status checks to include in the response (e.g. `withStatus=active`).

### Defining Ephemeral Environments

To mark a namespace as an ephemeral environment, add the label `envs.sberz.de/name: <environment-name>` to the namespace.

Add annotations to provide additional information about the environment:

- Annotation `url.envs.sberz.de/<endpoint-name>: <url>`: Define URLs for different endpoints (e.g., API, dashboard, etc.).

#### Status Checks
You can define status checks for each environment using Prometheus queries. Status checks are defined in the service configuration and can be used to determine the health or activity of an environment. Each status check has a name, a Prometheus query, and configuration for matching the results to environments.
For example, to define status checks, you can use the following configuration:

```yaml
prometheus:
	address: http://prometheus.example.local:9090
statusChecks:
	healthy:
		kind: bulk
		query: min by (namespace) (kube_deployment_status_replicas_ready{namespace=~"env-.+"})
		matchOn: namespace
		matchLabel: namespace
		interval: 30s
		timeout: 2s
```

Alternatively, you can define a static status check using annotations on the namespace. This is useful to create dummy environments or to override the dynamic status check results:
```yaml
metadata:
  annotations:
	status.envs.sberz.de/active: "true"
```

#### Dynamic Metadata

In addition to annotations you can expose metadata that is resolved dynamically via Prometheus. Each metadata entry defines the query configuration and the expected data type (`string`, `bool`, `number`, or `timestamp`). String metadata can optionally specify `extractLabel` to pull the text value from a label on the matching sample.

```yaml
prometheus:
	address: http://prometheus.example.local:9090
metadata:
	owner:
		type: string
		kind: bulk
		query: sum by (namespace, owner) (kube_namespace_labels{})
		matchOn: namespace
		matchLabel: namespace
		extractLabel: owner
		interval: 5m
		timeout: 5s
```

Metadata is included when calling `GET /v1/environment/{name}` and can be used by clients to display ownership, lifecycle timestamps, or any other contextual information that is available via Prometheus metrics.

#### Example

To try it out, apply the manifest in the `examples/basic` directory:

```bash
kubectl apply -f examples/basic/manifests
```

This will add two environments: `test` and `Test-Env-2.0`.

The `GET /v1/environment` endpoint will return:

```json
{
	"environments": ["Test-Env-2.0", "test"]
}
```

The `GET /v1/environment/test` endpoint will return:

```json
{
	"status": {
		"active": false,
		"healthy": false
	},
	"statusUpdatedAt": {
		"active": "2025-10-11T20:30:00Z",
		"healthy": "2025-10-11T20:30:00Z"
	},
	"createdAt": "2025-10-11T20:30:00Z",
	"url": {
		"api": "https://api-test.example.com",
		"dashboard": "https://app-test.example.com"
	},
	"meta": {
		"owner": "team-mobile",
		"lastDeploy": "2025-10-11T19:55:00Z"
	},
	"name": "test",
	"namespace": "env-test"
}
```

The `GET /v1/environment/all` endpoint will return:

```json
{
	"environments": [
		{
			"status": {},
			"statusUpdatedAt": {},
			"createdAt": "2025-10-11T20:30:00Z",
			"url": {
				"api": "https://api-test.example.com",
				"dashboard": "https://app-test.example.com"
			},
			"name": "test",
			"namespace": "env-test"
		},
		{
			"status": {},
			"statusUpdatedAt": {},
			"createdAt": "2025-10-11T20:30:00Z",
			"url": {
				"api": "https://api-test2.example.com",
				"dashboard": "https://app-test2.example.com"
			},
			"name": "Test-Env-2.0",
			"namespace": "env-test2"
		}
	]
}
```

## Monitoring

The service can be instrumented with Prometheus metrics for monitoring and observability. Metrics are **disabled by default** and can be enabled by setting the `--metrics-port` flag:

```bash
go run ./cmd/autodiscovery --metrics-port 8090
```

This will expose Prometheus metrics at `http://localhost:8090/metrics`.

## Development

To run the service locally for development, you need a Kubernetes cluster (e.g. kind, minikube, etc.) and `kubectl` configured to access it.

There are a few Makefile targets to help with development. To get started quickly, you can use:

```bash
# Creates a local kubernetes cluster using kind and podman
make testing/setup

# Sets the kubeconfig to the kind cluster. This is needed for the service to access the cluster.
export KUBECONFIG="$(realpath ./kind-kubeconfig.yaml)"

# Apply the example manifests to the cluster
kubectl apply -f examples/basic/manifests

# Run the service locally
go run ./cmd/autodiscovery --log-level debug

# Cleanup the kind cluster
make testing/teardown
```

To run the helm chart with the service in the cluster, you can use:
```bash
make testing/install-helm
```

This will build the container image as `ghcr.io/sberz/ephemeral-envs:local`, load it into the kind cluster, and install the helm chart with the local image.
