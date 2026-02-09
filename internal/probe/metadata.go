package probe

import (
	"context"
	"fmt"
	"time"

	"github.com/sberz/ephemeral-envs/internal/prometheus"
)

var (
	ErrInvalidType = fmt.Errorf("invalid type")
)

type MetadataType string

const (
	MetadataTypeString    MetadataType = "string"
	MetadataTypeBool      MetadataType = "bool"
	MetadataTypeNumber    MetadataType = "number"
	MetadataTypeTimestamp MetadataType = "timestamp"
)

func (t MetadataType) Validate() error {
	switch t {
	case MetadataTypeString, MetadataTypeBool, MetadataTypeNumber, MetadataTypeTimestamp:
		return nil
	default:
		return fmt.Errorf("%w: %s", ErrInvalidType, t)
	}
}

// MetadataProbe returns metadata values as any type.
type MetadataProbe interface {
	Value(ctx context.Context) (any, error)
	LastUpdate() time.Time
}

// metadataProbe wraps any typed Probe to return values as any.
type metadataProbe[T Type] struct {
	probe Probe[T]
}

func (m *metadataProbe[T]) Value(ctx context.Context) (any, error) {
	val, err := m.probe.Value(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get metadata value: %w", err)
	}

	return val, nil
}

func (m *metadataProbe[T]) LastUpdate() time.Time {
	return m.probe.LastUpdate()
}

// WrapProbe wraps a typed probe to return metadata as any.
func WrapProbe[T Type](probe Probe[T]) MetadataProbe {
	return &metadataProbe[T]{probe: probe}
}

// MetadataProber is a factory for creating MetadataProbes. It allows adding environments to create probes that are specific to an environment.
type MetadataProber interface {
	AddEnvironment(name string, namespace string) (MetadataProbe, error)
}

// metadataProber is a Prober adapter that creates MetadataProbes from typed Probers. It holds a reference to the underlying typed Prober and creates MetadataProbes on demand.
type metadataProber[V Type] struct {
	Prober[V]
}

func (m *metadataProber[V]) AddEnvironment(name string, namespace string) (MetadataProbe, error) {
	probe, err := m.Prober.AddEnvironment(name, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to add environment to prober: %w", err)
	}
	return WrapProbe(probe), nil
}

// WrapProber creates a MetadataProber from a typed Prober. It passes through any error from the underlying prober creation and wraps the typed Prober to return MetadataProbes.
func WrapProber[V Type](prober Prober[V], err error) (MetadataProber, error) {
	if err != nil {
		return nil, fmt.Errorf("failed to create prober: %w", err)
	}

	return &metadataProber[V]{Prober: prober}, nil
}

func NewPrometheusMetadataProber(ctx context.Context, prom *prometheus.Prometheus, t MetadataType, cfg prometheus.QueryConfig) (MetadataProber, error) {
	switch t {
	case MetadataTypeString:
		return WrapProber(NewPrometheusProber(ctx, prom, cfg, PromValToString))
	case MetadataTypeBool:
		return WrapProber(NewPrometheusProber(ctx, prom, cfg, PromValToBool))
	case MetadataTypeNumber:
		return WrapProber(NewPrometheusProber(ctx, prom, cfg, PromValToFloat))
	case MetadataTypeTimestamp:
		return WrapProber(NewPrometheusProber(ctx, prom, cfg, PromValToDateTime))
	default:
		return nil, fmt.Errorf("%w: %q", ErrInvalidType, t)
	}
}
