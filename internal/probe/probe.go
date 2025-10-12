package probe

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

type Config struct {
	// Prometheus query to determine if the environment is active
	Query string `yaml:"query"`
	// Interval between probes
	Interval time.Duration `yaml:"interval"`
	// Timeout for each probe
	Timeout time.Duration `yaml:"timeout"`
}

type Type interface {
	~bool | ~float64 | ~string
}

type Prober[V Type] interface {
	AddEnvironment(name string, namespace string) (Probe[V], error)
	// Keep it simple for now and run queries within the probe and cache results
	// RemoveEnvironment(name string) error
	// Shutdown(ctx context.Context) error
}

type Probe[V Type] interface {
	Value(ctx context.Context) (V, error)
	LastUpdate() time.Time
	Destroy(ctx context.Context) error
}

func (c *Config) Validate() error {

	// The query must be a valid Template
	if c.Query == "" {
		return fmt.Errorf("query must be set: %w", errInvalidVal)
	}

	t, err := template.New("query").Parse(c.Query)
	if err != nil {
		return fmt.Errorf("query must be a valid template: %w", err)
	}
	t.Option("missingkey=error")
	err = t.Execute(io.Discard, map[string]string{"name": "test", "namespace": "env-test"})
	if err != nil {
		return fmt.Errorf("query template execution failed: %w", err)
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
