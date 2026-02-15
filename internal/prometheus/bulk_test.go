package prometheus

import (
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/common/model"
)

func TestBulkValueQueryCacheAndMatchBehavior(t *testing.T) {
	t.Parallel()

	var (
		mu    sync.Mutex
		calls int
	)

	prom, closeFn := newTestPrometheus(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()

		q := requestQueryValue(r, "query")
		if q != `sum(up) by (namespace)` {
			t.Fatalf("query = %q, want %q", q, `sum(up) by (namespace)`)
		}

		writePromResponse(w, `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"namespace":"ns-a"},"value":[1700000000,"1"]},{"metric":{"namespace":"ns-b"},"value":[1700000000,"0"]}]}}`)
	})
	defer closeFn()

	cfg := QueryConfig{
		Name:       "bulk",
		Kind:       QueryKindBulk,
		Query:      `sum(up) by (namespace)`,
		MatchOn:    QueryMatchOnNamespace,
		MatchLabel: "namespace",
		Interval:   1 * time.Minute,
		Timeout:    2 * time.Second,
	}

	q, err := NewBulkValueQuery(t.Context(), prom, cfg)
	if err != nil {
		t.Fatalf("NewBulkValueQuery() error = %v", err)
	}

	sampleA, err := q.queryForEnvironment(t.Context(), "env-a", "ns-a")
	if err != nil {
		t.Fatalf("queryForEnvironment(ns-a) error = %v", err)
	}
	if sampleA.Value != model.SampleValue(1) {
		t.Fatalf("sampleA.Value = %v, want 1", sampleA.Value)
	}

	sampleB, err := q.queryForEnvironment(t.Context(), "env-b", "ns-b")
	if err != nil {
		t.Fatalf("queryForEnvironment(ns-b) error = %v", err)
	}
	if sampleB.Value != model.SampleValue(0) {
		t.Fatalf("sampleB.Value = %v, want 0", sampleB.Value)
	}

	sampleMissing, err := q.queryForEnvironment(t.Context(), "env-c", "ns-c")
	if err != nil {
		t.Fatalf("queryForEnvironment(ns-c) error = %v", err)
	}
	if sampleMissing.Value != model.SampleValue(0) {
		t.Fatalf("sampleMissing.Value = %v, want 0", sampleMissing.Value)
	}

	mu.Lock()
	gotCalls := calls
	mu.Unlock()
	if gotCalls != 1 {
		t.Fatalf("calls = %d, want 1 (bulk cache should avoid re-query within interval)", gotCalls)
	}
}

func TestBulkValueQueryResultTypeError(t *testing.T) {
	t.Parallel()

	prom, closeFn := newTestPrometheus(t, func(w http.ResponseWriter, _ *http.Request) {
		writePromResponse(w, `{"status":"success","data":{"resultType":"matrix","result":[]}}`)
	})
	defer closeFn()

	cfg := QueryConfig{
		Name:       "bulk-bad-type",
		Kind:       QueryKindBulk,
		Query:      `sum(up) by (namespace)`,
		MatchOn:    QueryMatchOnNamespace,
		MatchLabel: "namespace",
		Interval:   1 * time.Minute,
		Timeout:    2 * time.Second,
	}

	q, err := NewBulkValueQuery(t.Context(), prom, cfg)
	if err != nil {
		t.Fatalf("NewBulkValueQuery() error = %v", err)
	}

	_, err = q.queryForEnvironment(t.Context(), "env", "ns")
	if !errors.Is(err, ErrResultNotParsable) {
		t.Fatalf("queryForEnvironment() error = %v, want ErrResultNotParsable", err)
	}
}
