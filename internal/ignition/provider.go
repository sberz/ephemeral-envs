package ignition

import (
	"context"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var ignitionTriggers = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "ephemeralenv_ignition_triggers_total",
	Help: "Total number of ignition trigger attempts",
}, []string{"provider", "environment", "namespace", "status"})

type instrumentedProvider struct {
	next         Provider
	providerName string
}

func (p *instrumentedProvider) Trigger(ctx context.Context, req TriggerRequest) error {
	err := p.next.Trigger(ctx, req)
	if err != nil {
		ignitionTriggers.WithLabelValues(p.providerName, req.Environment, req.Namespace, "error").Inc()
		return fmt.Errorf("provider trigger failed: %w", err)
	}

	ignitionTriggers.WithLabelValues(p.providerName, req.Environment, req.Namespace, "accepted").Inc()
	return nil
}

func NewProvider(cfg *ProviderConfig) (Provider, error) {
	if cfg == nil {
		return nil, ErrProviderConfigRequired
	}

	switch cfg.Type {
	case ProviderTypePrometheus:
		if cfg.Prometheus == nil {
			cfg.Prometheus = &PrometheusProviderConfig{}
		}
		return &instrumentedProvider{
			providerName: string(cfg.Type),
			next:         NewPrometheusProvider(cfg.Prometheus),
		}, nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedProviderType, cfg.Type)
	}
}
