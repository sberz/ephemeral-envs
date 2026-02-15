package ignition

import (
	"context"
	"errors"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var ignitionRequestedAt = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "ephemeralenv_last_ignition_requested",
	Help: "Unix timestamp of the latest ignition trigger request",
}, []string{"environment", "namespace"})

var ErrEnvironmentRequired = errors.New("environment is required")

type PrometheusProvider struct{}

func NewPrometheusProvider(_ *PrometheusProviderConfig) *PrometheusProvider {
	return &PrometheusProvider{}
}

func (p *PrometheusProvider) Trigger(_ context.Context, req TriggerRequest) error {
	if req.Environment == "" {
		return ErrEnvironmentRequired
	}

	ignitionRequestedAt.WithLabelValues(req.Environment, req.Namespace).Set(float64(time.Now().Unix()))
	return nil
}
