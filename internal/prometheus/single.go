package prometheus

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"text/template"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

type SingleValueQuery struct {
	Prometheus *Prometheus
	QueryTpl   *template.Template
	cfg        QueryConfig
}

var _ EnvironmentQuerier = (*SingleValueQuery)(nil)

// NewSingleValueQuery creates a Prometheus query that expects a single value result.
func NewSingleValueQuery(ctx context.Context, prom Prometheus, cfg QueryConfig) (*SingleValueQuery, error) {
	err := cfg.Validate()
	if err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	if cfg.Kind != QueryKindSingleValue {
		return nil, fmt.Errorf("%w: %s for single value query", ErrInvalidQueryKind, cfg.Kind)
	}

	t, err := template.New("query").Option("missingkey=error").Parse(cfg.Query)
	if err != nil {
		return nil, fmt.Errorf("failed to parse query template: %w", err)
	}

	slog.DebugContext(ctx, "creating single value Prometheus query", "name", cfg.Name, "query_kind", cfg.Kind, "query", cfg.Query, "interval", cfg.Interval.String(), "timeout", cfg.Timeout.String())

	return &SingleValueQuery{
		Prometheus: &prom,
		QueryTpl:   t,
		cfg:        cfg,
	}, nil
}

func (q *SingleValueQuery) AddEnvironment(name string, namespace string) (QueryExecutor, error) {
	return &environmentQuery{
		query:     q,
		envName:   name,
		namespace: namespace,
	}, nil
}

func (q *SingleValueQuery) Config() QueryConfig {
	return q.cfg
}

func (q *SingleValueQuery) queryForEnvironment(ctx context.Context, name string, namespace string) (model.Sample, error) {
	start := time.Now()
	queryStatus := "failed"
	defer func() {
		promQueryDuration.WithLabelValues(q.cfg.Name, string(q.cfg.Kind), queryStatus).Observe(time.Since(start).Seconds())
	}()

	log := slog.With("name", q.cfg.Name, "query_kind", q.cfg.Kind, "env_name", name, "env_namespace", namespace)
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
	log.DebugContext(ctx, "executing Prometheus query")

	res, warnings, err := q.Prometheus.apiClient.Query(
		ctx, query, time.Now(),
		v1.WithTimeout(q.cfg.Timeout),
		// Limit the results to 2 to detect if there are too many results (we expect 0 or 1)
		v1.WithLimit(2),
	)
	if err != nil {
		return model.ZeroSample, fmt.Errorf("query failed: %w", err)
	}
	if len(warnings) > 0 {
		log.WarnContext(ctx, "prometheus query succeeded with warnings", "warnings", warnings)
	}

	samples, ok := res.(model.Vector)
	if !ok {
		return model.ZeroSample, fmt.Errorf("unexpected result type %T: %w", res, ErrResultNotParsable)
	}
	if len(samples) == 0 {
		log.WarnContext(ctx, "prometheus query returned no results")
		return model.ZeroSample, ErrResultNotFound
	}
	if len(samples) > 1 {
		log.ErrorContext(ctx, "prometheus query returned too many results", "num_results", len(samples), "results", samples)
		return model.ZeroSample, ErrTooManyResults
	}

	log.DebugContext(ctx, "prometheus query returned a result", "result", samples[0])

	if time.Since(samples[0].Timestamp.Time()).Abs() > sampleDriftAllowance {
		log.WarnContext(ctx, "prometheus query result is stale", "result_timestamp", samples[0].Timestamp.Time())
	}

	queryStatus = "success"
	return *samples[0], nil
}
