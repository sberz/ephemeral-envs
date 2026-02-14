package prometheus

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	promapi "github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
)

func newTestPrometheus(t *testing.T, handler func(http.ResponseWriter, *http.Request)) (Prometheus, func()) {
	t.Helper()

	ts := httptest.NewServer(http.HandlerFunc(handler))

	client, err := promapi.NewClient(promapi.Config{Address: ts.URL})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	return Prometheus{apiClient: v1.NewAPI(client)}, func() {
		ts.Close()
	}
}

func writePromResponse(w http.ResponseWriter, json string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprint(w, json)
}

func requestQueryValue(r *http.Request, key string) string {
	if err := r.ParseForm(); err == nil {
		if val := r.Form.Get(key); val != "" {
			return val
		}
	}

	return r.URL.Query().Get(key)
}
