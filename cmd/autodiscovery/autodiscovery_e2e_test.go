package main

import (
	"bytes"
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

type e2eEnvironmentResponse struct {
	CreatedAt time.Time         `json:"createdAt"`
	Status    map[string]bool   `json:"status"`
	URL       map[string]string `json:"url"`
	Meta      map[string]any    `json:"meta,omitempty"`
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
}

func TestE2ENamespaceLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping end-to-end test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	clientset, err := kube.GetClient()
	if err != nil {
		t.Fatalf("GetClient() error = %v", err)
	}

	promAddress := resolvePrometheusAddress()

	httpClient := &http.Client{Timeout: 10 * time.Second}
	waitFor(t, ctx, 15*time.Second, e2eWaitInterval, func() bool {
		return statusOK(ctx, httpClient, strings.TrimRight(promAddress, "/")+"/api/v1/status/buildinfo")
	})

	baseURL, metricsURL, httpClient := startE2EService(t, ctx, promAddress)

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
		deleteNamespaceIfExists(t, ctx, clientset, namespace)
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

		waitFor(t, ctx, e2eWaitTimeout, e2eWaitInterval, func() bool {
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

	t.Run("ignition endpoint accepts and updates metric", func(t *testing.T) {
		if err := requestStatus(ctx, httpClient, http.MethodPost, baseURL+"/v1/environment/"+envName+"/ignition", http.StatusAccepted); err != nil {
			t.Fatalf("ignition trigger request error = %v", err)
		}

		waitFor(t, ctx, e2eWaitTimeout, e2eWaitInterval, func() bool {
			metrics, err := getText(ctx, httpClient, metricsURL+"/metrics")
			if err != nil {
				return false
			}

			hasRequestedAt := strings.Contains(metrics, fmt.Sprintf(`ephemeralenv_last_ignition_requested{environment=%q,namespace=%q}`, envName, namespace))
			hasTriggerCount := strings.Contains(metrics, fmt.Sprintf(`ephemeralenv_ignition_triggers_total{environment=%q,namespace=%q,provider="prometheus",status="accepted"} 1`, envName, namespace))

			return hasRequestedAt && hasTriggerCount
		})

		if err := requestStatus(ctx, httpClient, http.MethodPost, baseURL+"/v1/environment/missing-ignition/ignition", http.StatusNotFound); err != nil {
			t.Fatalf("ignition trigger missing environment request error = %v", err)
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
			deleteNamespaceIfExists(t, ctx, clientset, namespace2)
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
		deleteNamespaceIfExists(t, ctx, clientset, namespace)
		waitForEnvironmentAbsent(t, ctx, httpClient, baseURL, envName)
	})
}

func startE2EService(t *testing.T, ctx context.Context, promAddress string) (string, string, *http.Client) {
	t.Helper()

	ctx, cancel := context.WithCancel(ctx)

	port := reserveFreePort(t, ctx)
	metricsPort := reserveFreePort(t, ctx)
	configPath := writeConfigFile(t, promAddress)

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	metricsURL := fmt.Sprintf("http://127.0.0.1:%d", metricsPort)
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
			"--metrics-port", strconv.Itoa(metricsPort),
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

	waitFor(t, ctx, 15*time.Second, e2eWaitInterval, func() bool {
		return statusOK(ctx, httpClient, baseURL+"/health")
	})
	waitFor(t, ctx, 15*time.Second, e2eWaitInterval, func() bool {
		return statusOK(ctx, httpClient, metricsURL+"/metrics")
	})

	return baseURL, metricsURL, httpClient
}

func resolvePrometheusAddress() string {
	promAddress := os.Getenv("E2E_PROMETHEUS_ADDRESS")
	if promAddress == "" {
		return defaultPrometheusAddress
	}

	return promAddress
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

func deleteNamespaceIfExists(t *testing.T, ctx context.Context, clientset *kubernetes.Clientset, namespace string) {
	t.Helper()

	err := clientset.CoreV1().Namespaces().Delete(ctx, namespace, metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		t.Fatalf("delete namespace %q error = %v", namespace, err)
	}
}

func waitForEnvironmentListed(t *testing.T, ctx context.Context, client *http.Client, baseURL string, envName string) {
	t.Helper()

	waitFor(t, ctx, e2eWaitTimeout, e2eWaitInterval, func() bool {
		var list e2eListResponse
		if err := getJSON(ctx, client, baseURL+"/v1/environment", &list); err != nil {
			return false
		}

		return slices.Contains(list.Environments, envName)
	})
}

func waitForEnvironmentAbsent(t *testing.T, ctx context.Context, client *http.Client, baseURL string, envName string) {
	t.Helper()

	waitFor(t, ctx, e2eWaitTimeout, e2eWaitInterval, func() bool {
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
	waitFor(t, ctx, e2eWaitTimeout, e2eWaitInterval, func() bool {
		if err := getJSON(ctx, client, baseURL+"/v1/environment/"+envName, &env); err != nil {
			return false
		}

		return ready(env)
	})

	return env
}

func reserveFreePort(t *testing.T, ctx context.Context) int {
	t.Helper()

	l, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
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

func waitFor(t *testing.T, ctx context.Context, timeout time.Duration, interval time.Duration, condition func() bool) {
	t.Helper()

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		if condition() {
			return
		}

		select {
		case <-waitCtx.Done():
			if errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
				t.Fatalf("timeout after %s waiting for condition", timeout)
			}

			t.Fatalf("context done while waiting for condition: %v", ctx.Err())
		case <-ticker.C:
		}
	}
}

func statusOK(ctx context.Context, client *http.Client, url string) bool {
	_, err := requestBody(ctx, client, http.MethodGet, url, http.StatusOK)
	return err == nil
}

func getJSON(ctx context.Context, client *http.Client, url string, out any) error {
	body, err := requestBody(ctx, client, http.MethodGet, url, http.StatusOK)
	if err != nil {
		return err
	}

	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errUnexpectedTrailing
	}

	return nil
}

func requestStatus(ctx context.Context, client *http.Client, method string, url string, wantStatus int) error {
	_, err := requestBody(ctx, client, method, url, wantStatus)
	return err
}

func getText(ctx context.Context, client *http.Client, url string) (string, error) {
	body, err := requestBody(ctx, client, http.MethodGet, url, http.StatusOK)
	if err != nil {
		return "", err
	}

	return string(body), nil
}

func requestBody(ctx context.Context, client *http.Client, method string, url string, wantStatus int) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != wantStatus {
		return nil, fmt.Errorf("%w: status=%d want=%d body=%s", errUnexpectedHTTPStatus, resp.StatusCode, wantStatus, strings.TrimSpace(string(body)))
	}

	return body, nil
}
