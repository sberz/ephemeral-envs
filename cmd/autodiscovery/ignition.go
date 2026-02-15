package main

import (
	"context"
	"fmt"

	"github.com/sberz/ephemeral-envs/internal/ignition"
)

func setupIgnitionProvider(_ context.Context, cfg *serviceConfig) (ignition.Provider, error) {
	providerCfg := &ignition.ProviderConfig{Type: ignition.ProviderTypePrometheus}
	if cfg.Ignition != nil && !cfg.Ignition.IsZero() {
		providerCfg = cfg.Ignition
	}

	provider, err := ignition.NewProvider(providerCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize ignition provider: %w", err)
	}

	return provider, nil
}
