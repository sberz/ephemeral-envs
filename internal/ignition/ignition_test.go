package ignition

import (
	"context"
	"testing"
)

func TestProviderConfigValidate(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		cfg     *ProviderConfig
		wantErr bool
	}{
		"valid prometheus provider": {
			cfg:     &ProviderConfig{Type: ProviderTypePrometheus},
			wantErr: false,
		},
		"missing type is treated as empty config": {
			cfg:     &ProviderConfig{},
			wantErr: false,
		},
		"unsupported provider type": {
			cfg:     &ProviderConfig{Type: ProviderType("keda")},
			wantErr: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := tt.cfg.Validate()
			if tt.wantErr && err == nil {
				t.Fatal("Validate() error = nil, want non-nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestNewProvider(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		cfg     *ProviderConfig
		wantErr bool
	}{
		"creates default prometheus provider": {
			cfg: &ProviderConfig{Type: ProviderTypePrometheus},
		},
		"rejects unsupported provider type": {
			cfg:     &ProviderConfig{Type: ProviderType("keda")},
			wantErr: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			provider, err := NewProvider(tt.cfg)
			if tt.wantErr {
				if err == nil {
					t.Fatal("NewProvider() error = nil, want non-nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("NewProvider() error = %v", err)
			}
			if provider == nil {
				t.Fatal("NewProvider() provider = nil, want non-nil")
			}
		})
	}
}

func TestPrometheusProviderTrigger(t *testing.T) {
	t.Parallel()

	provider := NewPrometheusProvider(&PrometheusProviderConfig{})
	tests := map[string]struct {
		req     TriggerRequest
		wantErr bool
	}{
		"fails for empty environment": {
			req:     TriggerRequest{Namespace: "ns"},
			wantErr: true,
		},
		"accepts valid request": {
			req: TriggerRequest{Environment: "env", Namespace: "ns"},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := provider.Trigger(context.Background(), tt.req)
			if tt.wantErr && err == nil {
				t.Fatal("Trigger() error = nil, want non-nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Trigger() error = %v", err)
			}
		})
	}
}
