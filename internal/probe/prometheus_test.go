package probe

import (
	"context"
	"errors"
	"testing"
	"time"

	prom "github.com/sberz/ephemeral-envs/internal/prometheus"
)

var errTestConvertFailed = errors.New("convert failed")

func TestNewPrometheusProbeNilValidation(t *testing.T) {
	t.Parallel()

	exec := &fakeQueryExecutor{value: 1, text: "1"}

	tests := []struct {
		queryExec prom.QueryExecutor
		converter ConverterFunc[bool]
		name      string
	}{
		{name: "nil query executor", queryExec: nil, converter: PromValToBool},
		{name: "nil converter", queryExec: exec, converter: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := NewPrometheusProbe[bool](tt.queryExec, tt.converter); !errors.Is(err, ErrInvalidNil) {
				t.Fatalf("NewPrometheusProbe() error = %v, want ErrInvalidNil", err)
			}
		})
	}
}

func TestPrometheusProbeValueAndConversion(t *testing.T) {
	t.Parallel()

	exec := &fakeQueryExecutor{value: 42, text: "ignored", updatedAtUnix: 1700000000}

	p, err := NewPrometheusProbe[intLikeFloat](exec, func(v float64, _ string) (intLikeFloat, error) {
		return intLikeFloat(v / 2), nil
	})
	if err != nil {
		t.Fatalf("NewPrometheusProbe() error = %v", err)
	}

	got, err := p.Value(t.Context())
	if err != nil {
		t.Fatalf("Value() error = %v", err)
	}

	if got != intLikeFloat(21) {
		t.Fatalf("Value() = %v, want %v", got, intLikeFloat(21))
	}

	wantUpdate := time.Unix(exec.updatedAtUnix, 0).UTC()
	if !p.LastUpdate().Equal(wantUpdate) {
		t.Fatalf("LastUpdate() = %v, want %v", p.LastUpdate(), wantUpdate)
	}
}

func TestPrometheusProbeValueErrorPaths(t *testing.T) {
	t.Parallel()

	t.Run("value error", func(t *testing.T) {
		t.Parallel()

		exec := &fakeQueryExecutor{valueErr: context.DeadlineExceeded}
		p, err := NewPrometheusProbe[bool](exec, PromValToBool)
		if err != nil {
			t.Fatalf("NewPrometheusProbe() error = %v", err)
		}

		if _, err := p.Value(t.Context()); err == nil {
			t.Fatal("Value() error = nil, want non-nil")
		}
	})

	t.Run("text error", func(t *testing.T) {
		t.Parallel()

		exec := &fakeQueryExecutor{value: 1, textErr: context.Canceled}
		p, err := NewPrometheusProbe[bool](exec, PromValToBool)
		if err != nil {
			t.Fatalf("NewPrometheusProbe() error = %v", err)
		}

		if _, err := p.Value(t.Context()); err == nil {
			t.Fatal("Value() error = nil, want non-nil")
		}
	})

	t.Run("converter error", func(t *testing.T) {
		t.Parallel()

		exec := &fakeQueryExecutor{value: 1, text: "x"}
		p, err := NewPrometheusProbe[bool](exec, func(float64, string) (bool, error) {
			return false, errTestConvertFailed
		})
		if err != nil {
			t.Fatalf("NewPrometheusProbe() error = %v", err)
		}

		if _, err := p.Value(t.Context()); err == nil {
			t.Fatal("Value() error = nil, want non-nil")
		}
	})
}

func TestNewPrometheusProberValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		wantErr   error
		prom      *prom.Prometheus
		converter ConverterFunc[bool]
		name      string
		cfg       prom.QueryConfig
	}{
		{
			name:      "nil prometheus",
			prom:      nil,
			cfg:       prom.QueryConfig{Kind: prom.QueryKindSingleValue},
			converter: PromValToBool,
			wantErr:   ErrInvalidNil,
		},
		{
			name:      "nil converter",
			prom:      &prom.Prometheus{},
			cfg:       prom.QueryConfig{Kind: prom.QueryKindSingleValue},
			converter: nil,
			wantErr:   ErrInvalidNil,
		},
		{
			name:      "invalid query kind",
			prom:      &prom.Prometheus{},
			cfg:       prom.QueryConfig{Kind: prom.QueryKind("invalid")},
			converter: PromValToBool,
			wantErr:   prom.ErrInvalidQueryKind,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := NewPrometheusProber[bool](t.Context(), tt.prom, tt.cfg, tt.converter); !errors.Is(err, tt.wantErr) {
				t.Fatalf("NewPrometheusProber() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

type intLikeFloat float64

type fakeQueryExecutor struct {
	valueErr      error
	textErr       error
	text          string
	updatedAtUnix int64
	value         float64
}

func (f *fakeQueryExecutor) Value(_ context.Context) (float64, error) {
	if f.valueErr != nil {
		return 0, f.valueErr
	}
	return f.value, nil
}

func (f *fakeQueryExecutor) Text(_ context.Context) (string, error) {
	if f.textErr != nil {
		return "", f.textErr
	}
	return f.text, nil
}

func (f *fakeQueryExecutor) LastUpdate() time.Time {
	if f.updatedAtUnix == 0 {
		return time.Time{}
	}

	return time.Unix(f.updatedAtUnix, 0).UTC()
}
