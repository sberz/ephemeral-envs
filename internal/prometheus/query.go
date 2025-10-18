package prometheus

import (
	"cmp"
	"context"
	"fmt"
	"html/template"
	"io"
	"sync"
	"time"

	"github.com/prometheus/common/model"
)

var (
	errInvalidVal        = fmt.Errorf("invalid value")
	ErrResultNotFound    = fmt.Errorf("result not found")
	ErrTooManyResults    = fmt.Errorf("too many results")
	ErrResultNotParsable = fmt.Errorf("result not parseable")
)

type BaseQueryConfig struct {
	Query        string        `yaml:"query"`
	ExtractLabel string        `yaml:"extract_label,omitempty"`
	Interval     time.Duration `yaml:"interval"`
	Timeout      time.Duration `yaml:"timeout"`
}

type SingleValueQueryConfig struct {
	// Embed the base BaseQueryConfig
	BaseQueryConfig `yaml:",inline"`
}

type BulkQueryConfig struct {
	MatchLabel      string `yaml:"match_label"`
	BaseQueryConfig `yaml:",inline"`
}

type EnvironmentQuerier interface {
	// AddEnvironment registers a new environment to be queried.
	AddEnvironment(name string, namespace string) (QueryExecutor, error)
	// Config returns the base query configuration.
	Config() BaseQueryConfig
	// queryForEnvironment executes the query for the given environment, returning the raw Prometheus sample.
	// The environment must have been previously registered via AddEnvironment.
	queryForEnvironment(ctx context.Context, name string, namespace string) (model.Sample, error)
	// removeEnvironment deregisters the environment.
	removeEnvironment(ctx context.Context, name string, namespace string) error
}

// QueryExecutor is the interface for executing Prometheus queries and retrieving results.
// The QueryExecutor is responsible triggering the query (if needed) and caching the result.
// The QueryExecutor should ensure that queries are not executed more frequently than the configured interval.
// Implementations for bulkqueries might refresh the cache for all environments at once.
type QueryExecutor interface {
	// The raw Prometheus model value
	Value(ctx context.Context) (float64, error)
	// The string representation of the value, either the configured label value or the stringified value
	Text(ctx context.Context) (string, error)
	// LastUpdate returns the time of the last successful query
	LastUpdate() time.Time
	// Destroy deregisters the environment and cleans up any resources.
	Destroy(ctx context.Context) error
}

type environmentQuery struct {
	lastStored model.Sample
	lastUpdate time.Time
	query      EnvironmentQuerier
	name       string
	namespace  string
	registered bool
	mu         sync.RWMutex
}

var _ QueryExecutor = (*environmentQuery)(nil)

func (c BaseQueryConfig) Validate() error {

	// The query must be a valid Template
	if c.Query == "" {
		return fmt.Errorf("query must be set: %w", errInvalidVal)
	}

	if c.Interval <= 0 {
		return fmt.Errorf("interval must be greater than 0: %w", errInvalidVal)
	}
	if c.Timeout <= 0 {
		return fmt.Errorf("timeout must be greater than 0: %w", errInvalidVal)
	}
	if c.Timeout >= c.Interval {
		return fmt.Errorf("timeout must be less than interval: %w", errInvalidVal)
	}
	return nil
}

func (c SingleValueQueryConfig) Validate() error {
	err := c.BaseQueryConfig.Validate()
	if err != nil {
		return err
	}

	// The query must be a valid Template and only use the defined template fields
	t, err := template.New("query").Parse(c.Query)
	if err != nil {
		return fmt.Errorf("query must be a valid template: %w", err)
	}
	t.Option("missingkey=error")
	err = t.Execute(io.Discard, map[string]string{
		"name":      "test",
		"namespace": "default",
	})
	if err != nil {
		return fmt.Errorf("query template execution failed: %w", err)
	}

	return nil
}

func (c BulkQueryConfig) Validate() error {
	err := c.BaseQueryConfig.Validate()
	if err != nil {
		return err
	}

	if c.MatchLabel == "" {
		return fmt.Errorf("match_label must be set: %w", errInvalidVal)
	}

	// The query must be a valid Template and not use any template fields
	t, err := template.New("query").Parse(c.Query)
	if err != nil {
		return fmt.Errorf("query must be a valid template: %w", err)
	}
	t.Option("missingkey=error")

	err = t.Execute(io.Discard, nil)
	if err != nil {
		return fmt.Errorf("query template execution failed: %w", err)
	}

	return nil
}

func (q *environmentQuery) Value(ctx context.Context) (float64, error) {
	sample, err := q.sample(ctx)
	if err != nil {
		return 0, err
	}

	return float64(sample.Value), nil
}

func (q *environmentQuery) Text(ctx context.Context) (string, error) {
	sample, err := q.sample(ctx)
	if err != nil {
		return "", err
	}

	extract := model.LabelName(q.query.Config().ExtractLabel)
	label := string(sample.Metric[extract])

	return cmp.Or(label, sample.Value.String(), ""), nil
}

func (q *environmentQuery) sample(ctx context.Context) (model.Sample, error) {
	// Technically the first half only needs a read lock, but upgrading is messy
	// and prone to deadlocks. The cached operation are fast enough that this shouldn't
	// cause real performance issues.
	q.mu.Lock()
	defer q.mu.Unlock()

	if !q.registered {
		return model.ZeroSample, fmt.Errorf("environment not registered: %w", ErrResultNotFound)
	}

	// If the last query was recent enough, return the cached value
	if time.Since(q.lastUpdate) < q.query.Config().Interval {
		return q.lastStored, nil
	}

	// Need to perform a new query

	var sample model.Sample
	sample, err := q.query.queryForEnvironment(ctx, q.name, q.namespace)
	if err != nil {
		return model.ZeroSample, fmt.Errorf("failed to query Prometheus for value: %w", err)
	}

	q.lastStored = sample
	q.lastUpdate = time.Now()

	return sample, nil
}

func (q *environmentQuery) LastUpdate() time.Time {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return q.lastUpdate
}

func (q *environmentQuery) Destroy(ctx context.Context) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.lastStored = model.ZeroSample
	q.lastUpdate = time.Time{}
	q.registered = false

	return q.query.removeEnvironment(ctx, q.name, q.namespace)
}
