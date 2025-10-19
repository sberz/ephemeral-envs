package probe

import (
	"context"
	"time"
)

type StaticProbe[V Type] struct {
	value V
}

func NewStaticProbe[V Type](value V) StaticProbe[V] {
	return StaticProbe[V]{value: value}
}

var _ Probe[bool] = StaticProbe[bool]{}

func (p StaticProbe[V]) Value(_ context.Context) (V, error) {
	return p.value, nil
}

func (p StaticProbe[V]) LastUpdate() time.Time {
	// Static probe never updates
	return time.Time{}
}
