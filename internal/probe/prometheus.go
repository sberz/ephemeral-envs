package probe

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"text/template"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
)

type PrometheusConfig struct {
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

func prometheusAPI(ctx context.Context, cfg PrometheusConfig) (v1.API, error) {
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

func NewPrometheus(ctx context.Context, cfg PrometheusConfig) (*Prometheus, error) {
	apiClient, err := prometheusAPI(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create Prometheus API client: %w", err)
	}

	return &Prometheus{
		apiClient: apiClient,
	}, nil
}

func NewPrometheusProber[V Type](ctx context.Context, prom *Prometheus, name string, cfg Config) (*PrometheusProber[V], error) {
	query, err := template.New("query").Option("missingkey=error").Parse(cfg.Query)
	if err != nil {
		return nil, fmt.Errorf("query must be a valid template: %w", err)
	}

	prober := &PrometheusProber[V]{
		name:  name,
		prom:  prom,
		cfg:   cfg,
		query: query,
	}

	slog.DebugContext(ctx, "Added Prometheus prober", "name", name, "query", cfg.Query, "interval", cfg.Interval.String(), "timeout", cfg.Timeout.String())
	return prober, nil
}

type PrometheusProber[V Type] struct {
	prom  *Prometheus
	query *template.Template
	name  string
	cfg   Config
}

var _ Prober[bool] = (*PrometheusProber[bool])(nil)

func (p *PrometheusProber[V]) AddEnvironment(name string, namespace string) (Probe[V], error) {
	slog.DebugContext(context.TODO(), "Creating Prometheus probe", "prober", p.name, "env_name", name, "env_namespace", namespace)
	return &prometheusProbe[V]{
		prober:    p,
		name:      name,
		namespace: namespace,
	}, nil
}

type prometheusProbe[V Type] struct {
	lastUpdate time.Time
	value      V
	prober     *PrometheusProber[V]
	name       string
	namespace  string
}

var _ Probe[bool] = (*prometheusProbe[bool])(nil)

func (p *prometheusProbe[V]) Destroy(_ context.Context) error {
	// No resources to clean up
	return nil
}

func (p *prometheusProbe[V]) LastUpdate() time.Time {
	return p.lastUpdate
}

func (p *prometheusProbe[V]) Value(ctx context.Context) (V, error) {
	var zero V

	log := slog.With("prober", p.prober.name, "env_name", p.name, "env_namespace", p.namespace)

	// If the last update was within the interval, return the cached value
	if p.lastUpdate.Add(p.prober.cfg.Interval).After(time.Now()) {
		return p.value, nil
	}

	data := map[string]string{
		"name":      p.name,
		"namespace": p.namespace,
	}

	query := strings.Builder{}
	err := p.prober.query.Execute(&query, data)
	if err != nil {
		return zero, fmt.Errorf("failed to execute query template: %w", err)
	}

	p.lastUpdate = time.Now()
	p.value, err = p.query(ctx, log, query.String())
	if err != nil {
		return zero, fmt.Errorf("failed to query Prometheus for value: %w", err)
	}

	return p.value, nil
}

func (p *prometheusProbe[V]) query(ctx context.Context, log *slog.Logger, query string) (V, error) {
	var value V

	log = log.With("query", query)
	log.DebugContext(ctx, "Executing Prometheus query")

	result, warnings, err := p.prober.prom.apiClient.Query(
		ctx, query, time.Now(),
		v1.WithTimeout(p.prober.cfg.Timeout),
		// Limit the results to 2 to detect if there are too many results (we expect 0 or 1)
		v1.WithLimit(2),
	)
	if err != nil {
		return value, fmt.Errorf("query execution failed: %w", err)
	}
	if len(warnings) > 0 {
		log.WarnContext(ctx, "Prometheus query succeeded with warnings", "warnings", warnings)
	}

	resValue, err := p.parseQueryResult(ctx, log, result)
	if err != nil {
		return value, fmt.Errorf("failed to extract Prometheus result: %w", err)
	}

	return resValue, nil
}

func (p *prometheusProbe[V]) parseQueryResult(ctx context.Context, log *slog.Logger, result model.Value) (V, error) {
	var sample model.Sample
	var value V

	switch res := result.(type) {
	case model.Vector:
		if len(res) == 0 {
			log.WarnContext(ctx, "Prometheus query returned no results")
			return value, ErrResultNotFound
		}
		if len(res) > 1 {
			log.ErrorContext(ctx, "Prometheus query returned too many results", "num_results", len(res), "results", res)
			return value, ErrTooManyResults
		}
		sample = *res[0]

	case *model.Scalar:
		sample = model.Sample{
			Value:     res.Value,
			Timestamp: res.Timestamp,
		}
	default:
		log.ErrorContext(ctx, "Unexpected result type", "type", result.Type(), "value", result.String())
		return value, ErrResultNotParsable
	}

	log.DebugContext(ctx, "Extracted Prometheus query result", "sample", sample)

	err := parseValue(ctx, log, sample, &value)
	if err != nil {
		return value, fmt.Errorf("failed to parse value: %w", err)
	}

	log.DebugContext(ctx, "Parsed Prometheus query result", "value", value)

	return value, nil
}

func parseValue(ctx context.Context, log *slog.Logger, sample model.Sample, v any) error {
	switch val := v.(type) {
	case *bool:
		if sample.Value > 0 {
			*val = true
		}
	case *float64:
		*val = float64(sample.Value)
	case *string:
		*val = sample.Value.String()
	default:
		log.ErrorContext(ctx, "unsupported value type", "type", fmt.Sprintf("%T", v))
		// This should never happen due to the type constraints on V
		panic("unsupported value type")
	}

	return nil
}
