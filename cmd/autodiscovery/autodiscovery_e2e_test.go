//go:build e2e
// +build e2e

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sberz/ephemeral-envs/internal/kube"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

var (
	errUnexpectedHTTPStatus = errors.New("unexpected http status")
	errUnexpectedTrailing   = errors.New("unexpected trailing data in response")
)

const defaultPrometheusAddress = "http://prometheus.env-test.localhost:3000"

const (
	e2eWaitTimeout  = 10 * time.Second
	e2eWaitInterval = 100 * time.Millisecond
)

type e2eListResponse struct {
	Environments []string `json:"environments"`
}

//nolint:govet // Field order mirrors API JSON payload structure for test readability.
type e2eEnvironmentResponse struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	CreatedAt time.Time         `json:"createdAt"`
	Status    map[string]bool   `json:"status"`
	URL       map[string]string `json:"url"`
	Meta      map[string]any    `json:"meta,omitempty"`
}

func TestE2ENamespaceLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	clientset, err := kube.GetClient()
	if err != nil {
		t.Fatalf("GetClient() error = %v", err)
	}

	promAddress := os.Getenv("E2E_PROMETHEUS_ADDRESS")
	if promAddress == "" {
		promAddress = defaultPrometheusAddress
	}

	httpClient := &http.Client{Timeout: 10 * time.Second}
	waitFor(t, 15*time.Second, e2eWaitInterval, func() bool {
		return statusOK(ctx, httpClient, strings.TrimRight(promAddress, "/")+"/api/v1/status/buildinfo")
	})

	baseURL, httpClient := startE2EService(t, ctx, cancel, promAddress)

	runID := fmt.Sprintf("%d", time.Now().UnixNano())
	envName := "e2e-" + runID
	namespace := "env-e2e-" + runID
	owner := "team-e2e-" + runID

	createNamespace(t, ctx, clientset, namespace, envName, map[string]string{
		AnnotationEnvURLPrefix + "api":            "https://api." + envName + ".example.test",
		AnnotationEnvURLPrefix + "dashboard":      "https://app." + envName + ".example.test",
		AnnotationEnvStatusCheckPrefix + "active": "true",
		AnnotationEnvMetadataPrefix + "owner":     fmt.Sprintf("%q", owner),
		AnnotationEnvMetadataPrefix + "version":   "1",
	})

	t.Cleanup(func() {
		deleteNamespaceIfExists(t, clientset, namespace)
	})

	waitForEnvironmentListed(t, ctx, httpClient, baseURL, envName)

	t.Run("list endpoint filters", func(t *testing.T) {
		var filtered e2eListResponse
		if err := getJSON(ctx, httpClient, baseURL+"/v1/environment?namespace="+namespace+"&status=active", &filtered); err != nil {
			t.Fatalf("get filtered environment list error = %v", err)
		}
		if len(filtered.Environments) != 1 || filtered.Environments[0] != envName {
			t.Fatalf("filtered environments = %#v, want [%q]", filtered.Environments, envName)
		}

		waitFor(t, e2eWaitTimeout, e2eWaitInterval, func() bool {
			var promFiltered e2eListResponse
			if err := getJSON(ctx, httpClient, baseURL+"/v1/environment?status=prom_ok", &promFiltered); err != nil {
				return false
			}

			return slices.Contains(promFiltered.Environments, envName)
		})
	})

	t.Run("environment endpoint resolves status and metadata", func(t *testing.T) {
		env := waitForEnvironmentReady(t, ctx, httpClient, baseURL, envName, func(env e2eEnvironmentResponse) bool {
			return env.Status["prom_ok"] && env.Meta["prom_build"] == float64(7)
		})

		if env.Name != envName || env.Namespace != namespace {
			t.Fatalf("environment identity = (%q, %q), want (%q, %q)", env.Name, env.Namespace, envName, namespace)
		}
		if env.URL["api"] != "https://api."+envName+".example.test" {
			t.Fatalf("api url = %q, want %q", env.URL["api"], "https://api."+envName+".example.test")
		}
		if !env.Status["active"] {
			t.Fatalf("status.active = %t, want true", env.Status["active"])
		}
		if env.Meta["owner"] != owner {
			t.Fatalf("meta.owner = %#v, want %q", env.Meta["owner"], owner)
		}
	})

	t.Run("unsupported metadata json falls back to literal string", func(t *testing.T) {
		runID2 := fmt.Sprintf("%d", time.Now().UnixNano())
		envName2 := "e2e-invalid-meta-" + runID2
		namespace2 := "env-e2e-invalid-meta-" + runID2
		ownerRaw := `{"team":"qa"}`

		createNamespace(t, ctx, clientset, namespace2, envName2, map[string]string{
			AnnotationEnvURLPrefix + "api":            "https://api." + envName2 + ".example.test",
			AnnotationEnvStatusCheckPrefix + "active": "true",
			AnnotationEnvMetadataPrefix + "owner":     ownerRaw,
		})

		t.Cleanup(func() {
			deleteNamespaceIfExists(t, clientset, namespace2)
		})

		waitForEnvironmentListed(t, ctx, httpClient, baseURL, envName2)

		env2 := waitForEnvironmentReady(t, ctx, httpClient, baseURL, envName2, func(env2 e2eEnvironmentResponse) bool {
			return env2.Meta["owner"] == ownerRaw
		})

		if env2.Meta["owner"] != ownerRaw {
			t.Fatalf("meta.owner = %#v, want literal %q", env2.Meta["owner"], ownerRaw)
		}
	})

	t.Run("namespace deletion removes environment", func(t *testing.T) {
		deleteNamespaceIfExists(t, clientset, namespace)
		waitForEnvironmentAbsent(t, ctx, httpClient, baseURL, envName)
	})
}

func startE2EService(t *testing.T, ctx context.Context, cancel context.CancelFunc, promAddress string) (string, *http.Client) {
	t.Helper()

	port := reserveFreePort(t)
	configPath := writeConfigFile(t, promAddress)

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	httpClient := &http.Client{Timeout: 10 * time.Second}
	errCh := make(chan error, 1)

	logLevel := "warn"
	if testing.Verbose() {
		logLevel = "debug"
	}

	go func() {
		errCh <- run(ctx, []string{
			"--log-level=" + logLevel,
			"--port", strconv.Itoa(port),
			"--config", configPath,
		})
	}()

	t.Cleanup(func() {
		cancel()
		select {
		case err := <-errCh:
			if err != nil {
				t.Fatalf("autodiscovery run() returned error: %v", err)
			}
		case <-time.After(15 * time.Second):
			t.Fatal("timeout waiting for run() to stop")
		}
	})

	waitFor(t, 15*time.Second, e2eWaitInterval, func() bool {
		return statusOK(ctx, httpClient, baseURL+"/health")
	})

	return baseURL, httpClient
}

func createNamespace(t *testing.T, ctx context.Context, clientset *kubernetes.Clientset, namespace string, envName string, annotations map[string]string) {
	t.Helper()

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name: namespace,
		Labels: map[string]string{
			LabelEnvName: envName,
		},
		Annotations: annotations,
	}}

	if _, err := clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create namespace error = %v", err)
	}
}

func deleteNamespaceIfExists(t *testing.T, clientset *kubernetes.Clientset, namespace string) {
	t.Helper()

	err := clientset.CoreV1().Namespaces().Delete(context.Background(), namespace, metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		t.Fatalf("delete namespace %q error = %v", namespace, err)
	}
}

func waitForEnvironmentListed(t *testing.T, ctx context.Context, client *http.Client, baseURL string, envName string) {
	t.Helper()

	waitFor(t, e2eWaitTimeout, e2eWaitInterval, func() bool {
		var list e2eListResponse
		if err := getJSON(ctx, client, baseURL+"/v1/environment", &list); err != nil {
			return false
		}

		return slices.Contains(list.Environments, envName)
	})
}

func waitForEnvironmentAbsent(t *testing.T, ctx context.Context, client *http.Client, baseURL string, envName string) {
	t.Helper()

	waitFor(t, e2eWaitTimeout, e2eWaitInterval, func() bool {
		var list e2eListResponse
		if err := getJSON(ctx, client, baseURL+"/v1/environment", &list); err != nil {
			return false
		}

		return !slices.Contains(list.Environments, envName)
	})
}

func waitForEnvironmentReady(
	t *testing.T,
	ctx context.Context,
	client *http.Client,
	baseURL string,
	envName string,
	ready func(env e2eEnvironmentResponse) bool,
) e2eEnvironmentResponse {
	t.Helper()

	var env e2eEnvironmentResponse
	waitFor(t, e2eWaitTimeout, e2eWaitInterval, func() bool {
		if err := getJSON(ctx, client, baseURL+"/v1/environment/"+envName, &env); err != nil {
			return false
		}

		return ready(env)
	})

	return env
}

func reserveFreePort(t *testing.T) int {
	t.Helper()

	l, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve free port: %v", err)
	}
	defer l.Close()

	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("unexpected listener addr type: %T", l.Addr())
	}

	return addr.Port
}

func writeConfigFile(t *testing.T, promURL string) string {
	t.Helper()

	content := fmt.Sprintf("prometheus:\n  address: %s\nstatusChecks:\n  prom_ok:\n    kind: single\n    query: vector(1)\n    interval: 2s\n    timeout: 1s\nmetadata:\n  prom_build:\n    type: number\n    kind: single\n    query: vector(7)\n    interval: 2s\n    timeout: 1s\n", promURL)

	path := filepath.Join(t.TempDir(), "e2e-config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	return path
}

func waitFor(t *testing.T, timeout time.Duration, interval time.Duration, condition func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		if condition() {
			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("timeout after %s waiting for condition", timeout)
		}
		time.Sleep(interval)
	}
}

func statusOK(ctx context.Context, client *http.Client, url string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}

	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

func getJSON(ctx context.Context, client *http.Client, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("%w: status=%d", errors.Join(errUnexpectedHTTPStatus, readErr), resp.StatusCode)
		}

		return fmt.Errorf("%w: status=%d body=%s", errUnexpectedHTTPStatus, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errUnexpectedTrailing
	}

	return nil
}
