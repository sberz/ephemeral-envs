package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/sberz/ephemeral-envs/internal/probe"
	promAPI "github.com/sberz/ephemeral-envs/internal/prometheus"
)

// setupProbers initializes status check and metadata probers from configuration.
func setupProbers(ctx context.Context, cfg *serviceConfig) (map[string]probe.Prober[bool], map[string]probe.MetadataProber, error) {
	statusChecks := make(map[string]probe.Prober[bool])
	metadata := make(map[string]probe.MetadataProber)

	if len(cfg.Prometheus.Address) == 0 {
		return statusChecks, metadata, nil
	}

	slog.DebugContext(ctx, "setting up Prometheus client", "url", cfg.Prometheus.Address)
	prometheus, err := promAPI.NewPrometheus(ctx, cfg.Prometheus)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create Prometheus client: %w", err)
	}

	for name, cfg := range cfg.StatusChecks {
		prober, err := probe.NewPrometheusProber(ctx, prometheus, *cfg, probe.PromValToBool)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create Prometheus prober for check %q: %w", name, err)
		}
		statusChecks[name] = prober
	}

	for name, metaCfg := range cfg.Metadata {
		prober, err := probe.NewPrometheusMetadataProber(ctx, prometheus, metaCfg.Type, metaCfg.QueryConfig)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create metadata prober for %q: %w", name, err)
		}
		metadata[name] = prober
	}

	return statusChecks, metadata, nil
}
