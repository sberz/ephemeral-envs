package prometheus

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/prometheus/common/model"
)

func TestSingleValueQueryQueryForEnvironment(t *testing.T) {
	t.Parallel()

	calls := 0
	prom, closeFn := newTestPrometheus(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/v1/query" {
			t.Fatalf("path = %q, want %q", r.URL.Path, "/api/v1/query")
		}

		q := requestQueryValue(r, "query")
		if q != `sum(up{namespace="env-ns"})` {
			t.Fatalf("query = %q, want %q", q, `sum(up{namespace="env-ns"})`)
		}

		writePromResponse(w, `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"owner":"team-a"},"value":[1700000000,"2"]}]}}`)
	})
	defer closeFn()

	cfg := QueryConfig{
		Name:     "healthy",
		Kind:     QueryKindSingleValue,
		Query:    `sum(up{namespace="{{.namespace}}"})`,
		Interval: 30 * time.Second,
		Timeout:  2 * time.Second,
	}

	q, err := NewSingleValueQuery(t.Context(), prom, cfg)
	if err != nil {
		t.Fatalf("NewSingleValueQuery() error = %v", err)
	}

	sample, err := q.queryForEnvironment(t.Context(), "env-a", "env-ns")
	if err != nil {
		t.Fatalf("queryForEnvironment() error = %v", err)
	}

	if sample.Value != model.SampleValue(2) {
		t.Fatalf("sample.Value = %v, want %v", sample.Value, model.SampleValue(2))
	}
	if string(sample.Metric[model.LabelName("owner")]) != "team-a" {
		t.Fatalf("sample owner label = %q, want %q", sample.Metric[model.LabelName("owner")], "team-a")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestSingleValueQueryErrorCases(t *testing.T) {
	t.Parallel()

	t.Run("no result", func(t *testing.T) {
		t.Parallel()

		prom, closeFn := newTestPrometheus(t, func(w http.ResponseWriter, _ *http.Request) {
			writePromResponse(w, `{"status":"success","data":{"resultType":"vector","result":[]}}`)
		})
		defer closeFn()

		cfg := QueryConfig{Name: "noresult", Kind: QueryKindSingleValue, Query: `vector(1)`, Interval: 30 * time.Second, Timeout: 2 * time.Second}
		q, err := NewSingleValueQuery(t.Context(), prom, cfg)
		if err != nil {
			t.Fatalf("NewSingleValueQuery() error = %v", err)
		}

		_, err = q.queryForEnvironment(t.Context(), "env", "ns")
		if !errors.Is(err, ErrResultNotFound) {
			t.Fatalf("queryForEnvironment() error = %v, want ErrResultNotFound", err)
		}
	})

	t.Run("too many results", func(t *testing.T) {
		t.Parallel()

		prom, closeFn := newTestPrometheus(t, func(w http.ResponseWriter, _ *http.Request) {
			writePromResponse(w, `{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1700000000,"1"]},{"metric":{},"value":[1700000001,"2"]}]}}`)
		})
		defer closeFn()

		cfg := QueryConfig{Name: "many", Kind: QueryKindSingleValue, Query: `vector(1)`, Interval: 30 * time.Second, Timeout: 2 * time.Second}
		q, err := NewSingleValueQuery(t.Context(), prom, cfg)
		if err != nil {
			t.Fatalf("NewSingleValueQuery() error = %v", err)
		}

		_, err = q.queryForEnvironment(t.Context(), "env", "ns")
		if !errors.Is(err, ErrTooManyResults) {
			t.Fatalf("queryForEnvironment() error = %v, want ErrTooManyResults", err)
		}
	})
}
