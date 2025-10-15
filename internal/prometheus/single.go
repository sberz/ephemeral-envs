package prometheus

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"text/template"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

type SingleValueQuery struct {
	Prometheus *Prometheus
	QueryTpl   *template.Template
	Config     SingleValueQueryConfig
}

var _ EnvironmentQuerier = (*SingleValueQuery)(nil)

type singleEnvQuery struct {
	lastStored model.Sample
	lastUpdate time.Time
	query      *SingleValueQuery
	name       string
	namespace  string
	mu         sync.RWMutex
}

var _ QueryExecutor = (*singleEnvQuery)(nil)

// NewSingleValueQuery creates a Prometheus query that expects a single value result.
func NewSingleValueQuery(ctx context.Context, prom Prometheus, cfg SingleValueQueryConfig) (*SingleValueQuery, error) {
	err := cfg.Validate()
	if err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	t, err := template.New("query").Option("missingkey=error").Parse(cfg.Query)
	if err != nil {
		return nil, fmt.Errorf("failed to parse query template: %w", err)
	}

	slog.DebugContext(ctx, "Creating single value Prometheus query", "query", cfg.Query, "interval", cfg.Interval.String(), "timeout", cfg.Timeout.String())

	return &SingleValueQuery{
		Prometheus: &prom,
		QueryTpl:   t,
		Config:     cfg,
	}, nil
}

func (q *SingleValueQuery) AddEnvironment(name string, namespace string) (QueryExecutor, error) {
	return &singleEnvQuery{
		query:     q,
		name:      name,
		namespace: namespace,
	}, nil
}

func (q *SingleValueQuery) QueryForEnvironment(ctx context.Context, name string, namespace string) (model.Sample, error) {
	log := slog.With("env_name", name, "env_namespace", namespace)
	tplData := map[string]string{
		"name":      name,
		"namespace": namespace,
	}

	var sb strings.Builder
	err := q.QueryTpl.Execute(&sb, tplData)
	if err != nil {
		return model.ZeroSample, fmt.Errorf("failed to execute query template: %w", err)
	}
	query := sb.String()

	log = log.With("query", query)
	log.DebugContext(ctx, "Executing Prometheus query")

	res, warnings, err := q.Prometheus.apiClient.Query(
		ctx, query, time.Now(),
		v1.WithTimeout(q.Config.Timeout),
		// Limit the results to 2 to detect if there are too many results (we expect 0 or 1)
		v1.WithLimit(2),
	)
	if err != nil {
		return model.ZeroSample, fmt.Errorf("query failed: %w", err)
	}
	if len(warnings) > 0 {
		log.WarnContext(ctx, "Prometheus query succeeded with warnings", "warnings", warnings)
	}

	samples, ok := res.(model.Vector)
	if !ok {
		return model.ZeroSample, fmt.Errorf("unexpected result type %T: %w", res, ErrResultNotParsable)
	}
	if len(samples) == 0 {
		log.WarnContext(ctx, "Prometheus query returned no results")
		return model.ZeroSample, ErrResultNotFound
	}
	if len(samples) > 1 {
		log.ErrorContext(ctx, "Prometheus query returned too many results", "num_results", len(samples), "results", samples)
		return model.ZeroSample, ErrTooManyResults
	}

	log.DebugContext(ctx, "Prometheus query returned a result", "result", samples[0])

	return *samples[0], nil
}

func (q *singleEnvQuery) Value(ctx context.Context) (float64, error) {
	sample, err := q.sample(ctx)
	if err != nil {
		return 0, err
	}

	return float64(sample.Value), nil
}

func (q *singleEnvQuery) Text(ctx context.Context) (string, error) {
	sample, err := q.sample(ctx)
	if err != nil {
		return "", err
	}

	extract := model.LabelName(q.query.Config.ExtractLabel)
	label := string(sample.Metric[extract])

	return cmp.Or(label, sample.Value.String(), ""), nil
}

func (q *singleEnvQuery) sample(ctx context.Context) (model.Sample, error) {
	// If the last query was recent enough, return the cached value
	// Only read-lock is needed here
	if time.Since(q.lastUpdate) < q.query.Config.Interval {
		q.mu.RLock()
		defer q.mu.RUnlock()
		return q.lastStored, nil
	}

	// Need to perform a new query - write-lock is needed
	q.mu.Lock()
	defer q.mu.Unlock()

	var sample model.Sample
	sample, err := q.query.QueryForEnvironment(ctx, q.name, q.namespace)
	if err != nil {
		return model.ZeroSample, fmt.Errorf("failed to query Prometheus for value: %w", err)
	}

	q.lastStored = sample
	q.lastUpdate = time.Now()

	return sample, nil

}

func (q *singleEnvQuery) LastUpdate() time.Time {
	return q.lastUpdate
}

func (q *singleEnvQuery) Destroy(_ context.Context) error {
	// No resources to clean up
	return nil
}
