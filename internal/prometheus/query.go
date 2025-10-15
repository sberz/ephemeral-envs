package prometheus

import (
	"context"
	"fmt"
	"html/template"
	"io"
	"time"
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

func (c *BaseQueryConfig) Validate() error {

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

func (c *SingleValueQueryConfig) Validate() error {
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

func (c *BulkQueryConfig) Validate() error {
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
