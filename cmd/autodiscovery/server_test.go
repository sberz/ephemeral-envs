package main

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sberz/ephemeral-envs/internal/probe"
	"github.com/sberz/ephemeral-envs/internal/store"
)

func TestParseStatusFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want  map[string]bool
		name  string
		url   string
		param string
	}{
		{
			name:  "empty query",
			url:   "/v1/environment",
			param: "status",
			want:  map[string]bool{},
		},
		{
			name:  "single positive filter",
			url:   "/v1/environment?status=healthy",
			param: "status",
			want: map[string]bool{
				"healthy": true,
			},
		},
		{
			name:  "mixed filters with spaces and empty values",
			url:   "/v1/environment?status=healthy,!active&status=%20ready%20,%20!%20,",
			param: "status",
			want: map[string]bool{
				"healthy": true,
				"active":  false,
				"ready":   true,
			},
		},
		{
			name:  "negative filter with inner spaces",
			url:   "/v1/environment?status=!%20ready%20",
			param: "status",
			want: map[string]bool{
				"ready": false,
			},
		},
		{
			name:  "different query key",
			url:   "/v1/environment/all?withStatus=deployed,!smoke",
			param: "withStatus",
			want: map[string]bool{
				"deployed": true,
				"smoke":    false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, tt.url, nil)
			got := parseStatusFilter(req, tt.param)

			if !maps.Equal(got, tt.want) {
				t.Fatalf("parseStatusFilter() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestHandleGetEnvironmentNotFound(t *testing.T) {
	t.Parallel()

	s := store.NewStore()
	mux := http.NewServeMux()
	mux.Handle("GET /v1/environment/{name}", handleGetEnvironment(s))

	req := httptest.NewRequest(http.MethodGet, "/v1/environment/missing", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestHandleGetEnvironmentOK(t *testing.T) {
	t.Parallel()

	s := newTestStoreWithEnvironments(t, newTestEnvironment("test", "env-test", true, false))

	mux := http.NewServeMux()
	mux.Handle("GET /v1/environment/{name}", handleGetEnvironment(s))

	req := httptest.NewRequest(http.MethodGet, "/v1/environment/test", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var got store.EnvironmentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if got.Name != "test" {
		t.Fatalf("name = %q, want %q", got.Name, "test")
	}

	if got.Meta["owner"] != "team-platform" {
		t.Fatalf("meta.owner = %#v, want %q", got.Meta["owner"], "team-platform")
	}
}

func TestHandleGetAllEnvironmentsWithStatusFilter(t *testing.T) {
	t.Parallel()

	s := store.NewStore()
	if err := s.AddEnvironment(t.Context(), newTestEnvironment("a", "env-a", true, false)); err != nil {
		t.Fatalf("AddEnvironment(a) error = %v", err)
	}
	if err := s.AddEnvironment(t.Context(), newTestEnvironment("b", "env-b", false, true)); err != nil {
		t.Fatalf("AddEnvironment(b) error = %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("GET /v1/environment/all", handleGetAllEnvironments(s))

	req := httptest.NewRequest(http.MethodGet, "/v1/environment/all?withStatus=healthy", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var got struct {
		Environments []store.EnvironmentResponse `json:"environments"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(got.Environments) != 2 {
		t.Fatalf("len(environments) = %d, want 2", len(got.Environments))
	}

	for _, env := range got.Environments {
		if len(env.Status) != 1 {
			t.Fatalf("env %q status count = %d, want 1", env.Name, len(env.Status))
		}
		if _, ok := env.Status["healthy"]; !ok {
			t.Fatalf("env %q missing healthy status in %#v", env.Name, env.Status)
		}
		if env.Meta != nil {
			t.Fatalf("env %q meta = %#v, want nil for /all endpoint", env.Name, env.Meta)
		}
	}
}

func TestHandleListEnvironmentNamesByNamespaceAndStatus(t *testing.T) {
	t.Parallel()

	s := newTestStoreWithEnvironments(
		t,
		newTestEnvironment("a", "env-a", true, false),
		newTestEnvironment("b", "env-b", false, true),
	)

	h := handleListEnvironmentNames(s)

	req := httptest.NewRequest(http.MethodGet, "/v1/environment?namespace=env-a&status=healthy", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var got struct {
		Environments []string `json:"environments"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(got.Environments) != 1 || got.Environments[0] != "a" {
		t.Fatalf("environments = %#v, want [\"a\"]", got.Environments)
	}
}

func TestHandleListEnvironmentNamesByNamespaceNotFound(t *testing.T) {
	t.Parallel()

	s := newTestStoreWithEnvironments(t, newTestEnvironment("a", "env-a", true, false))

	h := handleListEnvironmentNames(s)

	req := httptest.NewRequest(http.MethodGet, "/v1/environment?namespace=env-missing", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var got struct {
		Environments []string `json:"environments"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(got.Environments) != 0 {
		t.Fatalf("environments = %#v, want empty list", got.Environments)
	}
}

func TestHandleGetEnvironmentStatusProbeError(t *testing.T) {
	t.Parallel()

	s := newTestStoreWithEnvironments(t, store.Environment{
		Name:      "broken-status",
		Namespace: "env-broken-status",
		CreatedAt: time.Unix(1700000000, 0).UTC(),
		URL: map[string]string{
			"app": "https://example.test/broken-status",
		},
		StatusChecks: map[string]probe.Probe[bool]{
			"healthy": failingBoolProbe{},
		},
		MetaProbes: map[string]probe.MetadataProbe{},
	})

	mux := http.NewServeMux()
	mux.Handle("GET /v1/environment/{name}", handleGetEnvironment(s))

	req := httptest.NewRequest(http.MethodGet, "/v1/environment/broken-status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestHandleGetEnvironmentMetadataProbeError(t *testing.T) {
	t.Parallel()

	s := newTestStoreWithEnvironments(t, store.Environment{
		Name:      "broken-meta",
		Namespace: "env-broken-meta",
		CreatedAt: time.Unix(1700000000, 0).UTC(),
		URL: map[string]string{
			"app": "https://example.test/broken-meta",
		},
		StatusChecks: map[string]probe.Probe[bool]{
			"healthy": probe.NewStaticProbe(true),
		},
		MetaProbes: map[string]probe.MetadataProbe{
			"owner": failingMetadataProbe{},
		},
	})

	mux := http.NewServeMux()
	mux.Handle("GET /v1/environment/{name}", handleGetEnvironment(s))

	req := httptest.NewRequest(http.MethodGet, "/v1/environment/broken-meta", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestHandleGetAllEnvironmentsStatusProbeError(t *testing.T) {
	t.Parallel()

	s := newTestStoreWithEnvironments(t, store.Environment{
		Name:      "broken",
		Namespace: "env-broken",
		CreatedAt: time.Unix(1700000000, 0).UTC(),
		URL: map[string]string{
			"app": "https://example.test/broken",
		},
		StatusChecks: map[string]probe.Probe[bool]{
			"healthy": failingBoolProbe{},
		},
		MetaProbes: map[string]probe.MetadataProbe{},
	})

	mux := http.NewServeMux()
	mux.Handle("GET /v1/environment/all", handleGetAllEnvironments(s))

	req := httptest.NewRequest(http.MethodGet, "/v1/environment/all?withStatus=healthy", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestMiddlewareCORSPreflight(t *testing.T) {
	t.Parallel()

	nextCalled := false
	h := middlewareCORS(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		nextCalled = true
	}))

	req := httptest.NewRequest(http.MethodOptions, "/v1/environment", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}

	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("cors header = %q, want *", rec.Header().Get("Access-Control-Allow-Origin"))
	}

	if nextCalled {
		t.Fatal("next handler was called for preflight request")
	}
}

func TestMiddlewarePanicRecovery(t *testing.T) {
	t.Parallel()

	h := middlewarePanicRecovery(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/environment", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestHandleHealthCheck(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	handleHealthCheck().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	if rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("content-type = %q, want application/json", rec.Header().Get("Content-Type"))
	}

	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if got["status"] != "ok" {
		t.Fatalf("status field = %q, want ok", got["status"])
	}
}

func TestNewServerHandlerRoutingAndMiddleware(t *testing.T) {
	t.Parallel()

	h := NewServerHandler(newTestStoreWithEnvironments(t, newTestEnvironment("a", "env-a", true, false)))

	preflight := httptest.NewRequest(http.MethodOptions, "/v1/environment", nil)
	preflightRec := httptest.NewRecorder()
	h.ServeHTTP(preflightRec, preflight)

	if preflightRec.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want %d", preflightRec.Code, http.StatusNoContent)
	}
	if preflightRec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("preflight cors header = %q, want *", preflightRec.Header().Get("Access-Control-Allow-Origin"))
	}

	healthReq := httptest.NewRequest(http.MethodGet, "/health", nil)
	healthRec := httptest.NewRecorder()
	h.ServeHTTP(healthRec, healthReq)

	if healthRec.Code != http.StatusOK {
		t.Fatalf("health status = %d, want %d", healthRec.Code, http.StatusOK)
	}
	if healthRec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("health cors header = %q, want *", healthRec.Header().Get("Access-Control-Allow-Origin"))
	}

	envReq := httptest.NewRequest(http.MethodGet, "/v1/environment/a", nil)
	envRec := httptest.NewRecorder()
	h.ServeHTTP(envRec, envReq)

	if envRec.Code != http.StatusOK {
		t.Fatalf("env status = %d, want %d", envRec.Code, http.StatusOK)
	}
}

func newTestEnvironment(name string, namespace string, healthy bool, ready bool) store.Environment {
	return store.Environment{
		Name:      name,
		Namespace: namespace,
		CreatedAt: time.Unix(1700000000, 0).UTC(),
		URL: map[string]string{
			"app": "https://example.test/" + name,
		},
		StatusChecks: map[string]probe.Probe[bool]{
			"healthy": probe.NewStaticProbe(healthy),
			"ready":   probe.NewStaticProbe(ready),
		},
		MetaProbes: map[string]probe.MetadataProbe{
			"owner": probe.WrapProbe(probe.NewStaticProbe("team-platform")),
		},
	}
}

func newTestStoreWithEnvironments(t *testing.T, envs ...store.Environment) *store.Store {
	t.Helper()

	s := store.NewStore()
	for _, env := range envs {
		if err := s.AddEnvironment(t.Context(), env); err != nil {
			t.Fatalf("AddEnvironment(%s) error = %v", env.Name, err)
		}
	}

	return s
}

var errTestProbeFailed = errors.New("test probe failed")

type failingBoolProbe struct{}

func (f failingBoolProbe) Value(_ context.Context) (bool, error) {
	return false, errTestProbeFailed
}

func (f failingBoolProbe) LastUpdate() time.Time {
	return time.Time{}
}

type failingMetadataProbe struct{}

func (f failingMetadataProbe) Value(_ context.Context) (any, error) {
	return nil, errTestProbeFailed
}

func (f failingMetadataProbe) LastUpdate() time.Time {
	return time.Time{}
}
