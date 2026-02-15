package main

import (
	"testing"

	"github.com/sberz/ephemeral-envs/internal/ignition"
)

func TestSetupIgnitionProvider(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		cfg     *serviceConfig
		wantErr bool
	}{
		"missing ignition config uses default prometheus provider": {
			cfg: &serviceConfig{},
		},
		"ignition config without provider uses default prometheus provider": {
			cfg: &serviceConfig{Ignition: &ignition.ProviderConfig{}},
		},
		"explicit prometheus provider is enabled": {
			cfg: &serviceConfig{Ignition: &ignition.ProviderConfig{Type: ignition.ProviderTypePrometheus}},
		},
		"invalid explicit provider returns error": {
			cfg:     &serviceConfig{Ignition: &ignition.ProviderConfig{Type: ignition.ProviderType("keda")}},
			wantErr: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			provider, err := setupIgnitionProvider(t.Context(), tt.cfg)
			if tt.wantErr {
				if err == nil {
					t.Fatal("setupIgnitionProvider() error = nil, want non-nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("setupIgnitionProvider() error = %v", err)
			}

			if provider == nil {
				t.Fatal("provider = nil, want provider")
			}
		})
	}
}
