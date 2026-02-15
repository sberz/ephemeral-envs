package ignition

import (
	"errors"
	"fmt"
)

var (
	ErrUnsupportedProviderType = errors.New("unsupported provider type")
	ErrProviderConfigRequired  = errors.New("provider config is required")
)

type ProviderType string

const (
	ProviderTypePrometheus ProviderType = "prometheus"
)

func (p ProviderType) Validate() error {
	switch p {
	case ProviderTypePrometheus:
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrUnsupportedProviderType, p)
	}
}

type ProviderConfig struct {
	Prometheus *PrometheusProviderConfig `yaml:"prometheus,omitempty"`
	Type       ProviderType              `yaml:"type"`
}

type PrometheusProviderConfig struct{}

func (c *ProviderConfig) IsZero() bool {
	if c == nil {
		return true
	}

	return c.Type == "" && c.Prometheus == nil
}

func (c *ProviderConfig) Validate() error {
	if c == nil || c.IsZero() {
		return nil
	}
	if err := c.Type.Validate(); err != nil {
		return err
	}

	if c.Type == ProviderTypePrometheus && c.Prometheus == nil {
		c.Prometheus = &PrometheusProviderConfig{}
	}

	return nil
}
