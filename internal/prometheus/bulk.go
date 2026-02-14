package prometheus

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

type BulkValueQuery struct {
	lastQuery  time.Time
	Prometheus *Prometheus
	valCache   map[string]model.Sample
	cfg        QueryConfig
	mu         sync.Mutex
}

func NewBulkValueQuery(ctx context.Context, prom Prometheus, cfg QueryConfig) (*BulkValueQuery, error) {
	err := cfg.Validate()
	if err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	if cfg.Kind != QueryKindBulk {
		return nil, fmt.Errorf("%w: %s for bulk value query", ErrInvalidQueryKind, cfg.Kind)
	}

	slog.DebugContext(ctx, "creating bulk value Prometheus query", "name", cfg.Name, "query_kind", cfg.Kind, "query", cfg.Query, "interval", cfg.Interval.String(), "timeout", cfg.Timeout.String(), "match_on", cfg.MatchOn, "match_label", cfg.MatchLabel)

	return &BulkValueQuery{
		Prometheus: &prom,
		cfg:        cfg,
		valCache:   make(map[string]model.Sample),
	}, nil
}

func (q *BulkValueQuery) matchKey(name, namespace string) string {
	switch q.cfg.MatchOn {
	case QueryMatchOnEnvName:
		return name
	case QueryMatchOnNamespace:
		return namespace
	default:
		return ""
	}
}

func (q *BulkValueQuery) AddEnvironment(name string, namespace string) (QueryExecutor, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	return &environmentQuery{
		query:     q,
		envName:   name,
		namespace: namespace,
	}, nil

}

func (q *BulkValueQuery) Config() QueryConfig {
	return q.cfg
}

func (q *BulkValueQuery) queryForEnvironment(ctx context.Context, envName string, namespace string) (model.Sample, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	start := time.Now()
	queryStatus := "failed"
	defer func() {
		promQueryDuration.WithLabelValues(q.cfg.Name, string(q.cfg.Kind), queryStatus).Observe(time.Since(start).Seconds())
	}()

	log := slog.With("name", q.cfg.Name, "query_kind", q.cfg.Kind, "env_name", envName, "env_namespace", namespace, "query", q.cfg.Query)

	match := q.matchKey(envName, namespace)

	if time.Since(q.lastQuery) < q.cfg.Interval {
		queryStatus = "cached"
		val, ok := q.valCache[match]
		if ok {
			log.DebugContext(ctx, "using cached value for query", "match_key", match)
			return val, nil
		}

		log.DebugContext(ctx, "cached value not found for query, but interval has not elapsed", "match_key", match)
		return model.Sample{Timestamp: model.TimeFromUnixNano(q.lastQuery.UnixNano())}, nil
	}

	// Need to perform a new bulk query
	// reset the cache
	q.valCache = make(map[string]model.Sample)

	// Perform the bulk query
	log.DebugContext(ctx, "executing Prometheus query")
	res, warnings, err := q.Prometheus.apiClient.Query(
		ctx, q.cfg.Query, time.Now(),
		v1.WithTimeout(q.cfg.Timeout),
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
	}

	log.DebugContext(ctx, "prometheus query returned a result", "result", samples)

	// Map the samples to the environment queries
	for _, sample := range samples {
		key := string(sample.Metric[model.LabelName(q.cfg.MatchLabel)])

		if time.Since(sample.Timestamp.Time()).Abs() > sampleDriftAllowance {
			log.WarnContext(ctx, "prometheus query result is stale", "result_timestamp", sample.Timestamp.Time())
		}

		q.valCache[key] = *sample
	}
	q.lastQuery = time.Now()
	queryStatus = "success"

	val, ok := q.valCache[match]
	if !ok {
		// No result for this environment, don't treat as an error as the environment may legitimately have no data
		// i.e: during creation or if the probe condition is not met
		log.WarnContext(ctx, "no result for registered environment after bulk query", "match_key", match)
		return model.Sample{Timestamp: model.Now()}, nil
	}

	return val, nil
}
