package probe

import (
	"context"
	"fmt"
	"time"

	"github.com/sberz/ephemeral-envs/internal/prometheus"
)

var (
	ErrInvalidNil = fmt.Errorf("nil value provided")
)

type ConverterFunc[V Type] func(value float64, text string) (V, error)

type PrometheusProbe[V Type] struct {
	prometheus.QueryExecutor
	converter ConverterFunc[V]
}

var _ Probe[bool] = (*PrometheusProbe[bool])(nil)

type PrometheusProber[V Type] struct {
	query     prometheus.EnvironmentQuerier
	converter ConverterFunc[V]
}

var _ Prober[bool] = (*PrometheusProber[bool])(nil)

// NewPrometheusProber creates a prober that uses Prometheus to determine the value.
func NewPrometheusProber[V Type](ctx context.Context, prom *prometheus.Prometheus, cfg prometheus.QueryConfig, converter ConverterFunc[V]) (*PrometheusProber[V], error) {
	if prom == nil || converter == nil {
		return nil, fmt.Errorf("prom and converter must be provided: %w", ErrInvalidNil)
	}

	query, err := prometheus.NewSingleValueQuery(ctx, *prom, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create Prometheus query: %w", err)
	}

	prober := &PrometheusProber[V]{
		query:     query,
		converter: converter,
	}
	return prober, nil
}

func (p *PrometheusProber[V]) AddEnvironment(name string, namespace string) (Probe[V], error) {
	e, err := p.query.AddEnvironment(name, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to add environment: %w", err)
	}
	return NewPrometheusProbe[V](e, p.converter)
}

var (
	PromValToFloat = func(value float64, _ string) (float64, error) {
		return value, nil
	}
	PromValToString = func(_ float64, text string) (string, error) {
		return text, nil
	}
	PromValToBool = func(value float64, _ string) (bool, error) {
		if value == 0 {
			return false, nil
		}
		return true, nil
	}
	PromValToDateTime = func(value float64, _ string) (time.Time, error) {
		// Prometheus returns timestamps as float seconds since epoch
		return time.Unix(int64(value), 0), nil
	}
)

// NewPrometheusProbe creates a probe that uses Prometheus to determine the value.
func NewPrometheusProbe[V Type](queryExec prometheus.QueryExecutor, converter ConverterFunc[V]) (*PrometheusProbe[V], error) {
	if queryExec == nil || converter == nil {
		return nil, fmt.Errorf("queryExec and converter must be provided: %w", ErrInvalidNil)
	}

	probe := &PrometheusProbe[V]{
		QueryExecutor: queryExec,
		converter:     converter,
	}
	return probe, nil
}

func (p *PrometheusProbe[V]) Value(ctx context.Context) (V, error) {
	var zero V

	val, err := p.QueryExecutor.Value(ctx)
	if err != nil {
		return zero, fmt.Errorf("probe query execution failed: %w", err)
	}
	text, err := p.Text(ctx)
	if err != nil {
		return zero, fmt.Errorf("probe query text retrieval failed: %w", err)
	}

	sample, err := p.converter(val, text)
	if err != nil {
		return zero, fmt.Errorf("probe value conversion failed: %w", err)
	}
	return sample, nil
}
