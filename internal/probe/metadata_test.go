package probe

import (
	"context"
	"errors"
	"testing"
	"time"

	prom "github.com/sberz/ephemeral-envs/internal/prometheus"
)

var (
	errTestSetupFailed = errors.New("setup failed")
	errTestAddFailed   = errors.New("add failed")
)

func TestMetadataTypeValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		metadataType MetadataType
		wantErr      bool
	}{
		{name: "string", metadataType: MetadataTypeString},
		{name: "bool", metadataType: MetadataTypeBool},
		{name: "number", metadataType: MetadataTypeNumber},
		{name: "timestamp", metadataType: MetadataTypeTimestamp},
		{name: "invalid", metadataType: MetadataType("invalid"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.metadataType.Validate()
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidType) {
					t.Fatalf("Validate() error = %v, want ErrInvalidType", err)
				}
				return
			}

			if err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestWrapProbe(t *testing.T) {
	t.Parallel()

	inner := NewStaticProbe("team-a")
	wrapped := WrapProbe[string](inner)

	val, err := wrapped.Value(context.Background())
	if err != nil {
		t.Fatalf("WrapProbe().Value() error = %v", err)
	}
	if val != "team-a" {
		t.Fatalf("WrapProbe().Value() = %#v, want %#v", val, "team-a")
	}

	if !wrapped.LastUpdate().Equal(time.Time{}) {
		t.Fatalf("WrapProbe().LastUpdate() = %v, want zero time", wrapped.LastUpdate())
	}
}

func TestWrapProber(t *testing.T) {
	t.Parallel()

	inner := NewStaticProbe("team-a")
	p := &fakeTypedProber[string]{probe: inner}
	metaProber, err := WrapProber[string](p, nil)
	if err != nil {
		t.Fatalf("WrapProber() error = %v", err)
	}

	metaProbe, err := metaProber.AddEnvironment("a", "env-a")
	if err != nil {
		t.Fatalf("MetadataProber.AddEnvironment() error = %v", err)
	}

	metaVal, err := metaProbe.Value(context.Background())
	if err != nil {
		t.Fatalf("MetadataProbe.Value() error = %v", err)
	}
	if metaVal != "team-a" {
		t.Fatalf("MetadataProbe.Value() = %#v, want %#v", metaVal, "team-a")
	}

	if p.calls != 1 {
		t.Fatalf("fakeTypedProber calls = %d, want 1", p.calls)
	}
}

func TestWrapProberErrorPaths(t *testing.T) {
	t.Parallel()

	t.Run("setup error", func(t *testing.T) {
		t.Parallel()

		if _, err := WrapProber[string](nil, errTestSetupFailed); err == nil {
			t.Fatal("WrapProber(nil, err) error = nil, want non-nil")
		}
	})

	t.Run("add environment error", func(t *testing.T) {
		t.Parallel()

		p := &fakeTypedProber[string]{err: errTestAddFailed}
		metaProber, err := WrapProber[string](p, nil)
		if err != nil {
			t.Fatalf("WrapProber() error = %v", err)
		}

		if _, err := metaProber.AddEnvironment("a", "env-a"); err == nil {
			t.Fatal("MetadataProber.AddEnvironment() error = nil, want non-nil")
		}
	})
}

func TestNewPrometheusMetadataProberInvalidType(t *testing.T) {
	t.Parallel()

	cfg := prom.QueryConfig{}
	if _, err := NewPrometheusMetadataProber(context.Background(), &prom.Prometheus{}, MetadataType("invalid"), cfg); !errors.Is(err, ErrInvalidType) {
		t.Fatalf("NewPrometheusMetadataProber() error = %v, want ErrInvalidType", err)
	}
}

type fakeTypedProber[V Type] struct {
	probe Probe[V]
	err   error
	calls int
}

func (f *fakeTypedProber[V]) AddEnvironment(_, _ string) (Probe[V], error) {
	if f.err != nil {
		return nil, f.err
	}
	f.calls++
	return f.probe, nil
}
