package prometheus

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/common/model"
)

func TestQueryConfigValidateCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     QueryConfig
		wantErr bool
	}{
		{
			name: "valid single",
			cfg: QueryConfig{
				Name:     "healthy",
				Kind:     QueryKindSingleValue,
				Query:    `sum(up{namespace="{{.namespace}}"})`,
				Interval: 30 * time.Second,
				Timeout:  2 * time.Second,
			},
		},
		{
			name: "invalid kind",
			cfg: QueryConfig{
				Name:     "kind",
				Kind:     QueryKind("x"),
				Query:    "vector(1)",
				Interval: 30 * time.Second,
				Timeout:  2 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "timeout gte interval",
			cfg: QueryConfig{
				Name:     "timeout",
				Kind:     QueryKindSingleValue,
				Query:    "vector(1)",
				Interval: 5 * time.Second,
				Timeout:  5 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "single bad template variable",
			cfg: QueryConfig{
				Name:     "template",
				Kind:     QueryKindSingleValue,
				Query:    `sum(up{x="{{.unknown}}"})`,
				Interval: 30 * time.Second,
				Timeout:  2 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "bulk missing match label",
			cfg: QueryConfig{
				Name:     "bulk",
				Kind:     QueryKindBulk,
				Query:    `sum(up) by (namespace)`,
				MatchOn:  QueryMatchOnNamespace,
				Interval: 30 * time.Second,
				Timeout:  2 * time.Second,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestQueryConfigValidateRequiredFields(t *testing.T) {
	t.Parallel()

	base := QueryConfig{
		Name:     "healthy",
		Kind:     QueryKindSingleValue,
		Query:    "vector(1)",
		Interval: 30 * time.Second,
		Timeout:  2 * time.Second,
	}

	tests := []struct {
		name string
		cfg  QueryConfig
	}{
		{
			name: "missing name",
			cfg: func() QueryConfig {
				cfg := base
				cfg.Name = ""
				return cfg
			}(),
		},
		{
			name: "missing query",
			cfg: func() QueryConfig {
				cfg := base
				cfg.Query = ""
				return cfg
			}(),
		},
		{
			name: "non-positive interval",
			cfg: func() QueryConfig {
				cfg := base
				cfg.Interval = 0
				return cfg
			}(),
		},
		{
			name: "non-positive timeout",
			cfg: func() QueryConfig {
				cfg := base
				cfg.Timeout = 0
				return cfg
			}(),
		},
		{
			name: "bulk missing match label",
			cfg: QueryConfig{
				Name:     "bulk",
				Kind:     QueryKindBulk,
				Query:    "sum(up) by (namespace)",
				MatchOn:  QueryMatchOnNamespace,
				Interval: 30 * time.Second,
				Timeout:  2 * time.Second,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if err := tt.cfg.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want non-nil")
			}
		})
	}
}

func TestEnvironmentQueryCacheHitAndRefresh(t *testing.T) {
	t.Parallel()

	fq := &testQuerier{
		cfg: QueryConfig{
			Name:         "cached",
			Kind:         QueryKindSingleValue,
			Query:        "vector(1)",
			ExtractLabel: "owner",
			Interval:     20 * time.Millisecond,
			Timeout:      2 * time.Second,
		},
		sample: model.Sample{
			Metric: model.Metric{model.LabelName("owner"): "team-a"},
			Value:  model.SampleValue(1),
		},
	}

	q := &environmentQuery{query: fq, envName: "env", namespace: "ns"}
	ctx := t.Context()

	if _, err := q.Value(ctx); err != nil {
		t.Fatalf("first Value() error = %v", err)
	}
	if _, err := q.Value(ctx); err != nil {
		t.Fatalf("second Value() error = %v", err)
	}

	if fq.calls != 1 {
		t.Fatalf("calls after cache hit = %d, want 1", fq.calls)
	}

	text, err := q.Text(ctx)
	if err != nil {
		t.Fatalf("Text() error = %v", err)
	}
	if text != "team-a" {
		t.Fatalf("Text() = %q, want team-a", text)
	}

	time.Sleep(30 * time.Millisecond)

	if _, err := q.Value(ctx); err != nil {
		t.Fatalf("third Value() error = %v", err)
	}

	if fq.calls != 2 {
		t.Fatalf("calls after refresh = %d, want 2", fq.calls)
	}
}

func TestEnvironmentQueryTextUsesNumericValueByDefault(t *testing.T) {
	t.Parallel()

	fq := &testQuerier{
		cfg: QueryConfig{
			Name:     "raw-value",
			Kind:     QueryKindSingleValue,
			Query:    "vector(1)",
			Interval: 30 * time.Second,
			Timeout:  2 * time.Second,
		},
		sample: model.Sample{Value: model.SampleValue(42)},
	}

	q := &environmentQuery{query: fq, envName: "env", namespace: "ns"}

	got, err := q.Text(t.Context())
	if err != nil {
		t.Fatalf("Text() error = %v", err)
	}

	if got != "42" {
		t.Fatalf("Text() = %q, want %q", got, "42")
	}
}

func TestEnvironmentQueryValuePropagatesError(t *testing.T) {
	t.Parallel()

	fq := &testQuerier{
		cfg: QueryConfig{
			Name:     "broken",
			Kind:     QueryKindSingleValue,
			Query:    "vector(1)",
			Interval: 30 * time.Second,
			Timeout:  2 * time.Second,
		},
		err: context.DeadlineExceeded,
	}

	q := &environmentQuery{query: fq, envName: "env", namespace: "ns"}

	if _, err := q.Value(t.Context()); err == nil {
		t.Fatal("Value() error = nil, want non-nil")
	}
}

type testQuerier struct {
	sample model.Sample
	err    error
	cfg    QueryConfig
	calls  int
}

func (f *testQuerier) AddEnvironment(_, _ string) (QueryExecutor, error) {
	panic("AddEnvironment should not be called in this unit test")
}

func (f *testQuerier) Config() QueryConfig {
	return f.cfg
}

func (f *testQuerier) queryForEnvironment(_ context.Context, _, _ string) (model.Sample, error) {
	if f.err != nil {
		return model.ZeroSample, f.err
	}

	f.calls++
	f.sample.Timestamp = model.TimeFromUnixNano(time.Now().UnixNano())

	return f.sample, nil
}
