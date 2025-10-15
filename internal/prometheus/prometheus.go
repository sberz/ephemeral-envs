package prometheus

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/config"
)

type Config struct {
	// ClientConfig provides all Prometheus HTTTP authentication options
	ClientConfig config.HTTPClientConfig `yaml:"clientConfig,omitempty"`
	// Additional HTTP headers to include in requests to Prometheus API.
	// An easier way to set simple headers. This will override the headers in ClientConfig.
	Headers map[string]string `yaml:"headers,omitempty"`
	// The address of the Prometheus to connect to.
	Address string `yaml:"address"`
}

type Prometheus struct {
	apiClient v1.API
}

func prometheusAPI(ctx context.Context, cfg Config) (v1.API, error) {
	// Set headers from cfg.Headers into cfg.ClientConfig.HTTPHeaders
	if cfg.ClientConfig.HTTPHeaders == nil {
		cfg.ClientConfig.HTTPHeaders = &config.Headers{
			Headers: make(map[string]config.Header),
		}
	}
	for k, v := range cfg.Headers {
		cfg.ClientConfig.HTTPHeaders.Headers[k] = config.Header{Values: []string{v}}
	}

	httpClient, err := config.NewClientFromConfig(cfg.ClientConfig, "prometheus")
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	client, err := api.NewClient(api.Config{
		Address: cfg.Address,
		Client:  httpClient,
	})

	if err != nil {
		return nil, fmt.Errorf("invalid client: %w", err)
	}

	api := v1.NewAPI(client)
	res, err := api.Buildinfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("connection failed: %w", err)
	}

	slog.DebugContext(ctx, "Connected to Prometheus", "build_info", res)

	return api, nil
}

func NewPrometheus(ctx context.Context, cfg Config) (*Prometheus, error) {
	apiClient, err := prometheusAPI(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create Prometheus API client: %w", err)
	}

	return &Prometheus{
		apiClient: apiClient,
	}, nil
}
