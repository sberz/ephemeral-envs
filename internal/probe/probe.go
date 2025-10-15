package probe

import (
	"context"
	"time"
)

type Type interface {
	~bool | ~float64 | ~string
}

type Prober[V Type] interface {
	AddEnvironment(name string, namespace string) (Probe[V], error)
	// Keep it simple for now and run queries within the probe and cache results
	// RemoveEnvironment(name string) error
	// Shutdown(ctx context.Context) error
}

type Probe[V Type] interface {
	Value(ctx context.Context) (V, error)
	LastUpdate() time.Time
	Destroy(ctx context.Context) error
}
