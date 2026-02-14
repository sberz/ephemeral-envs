package store

import (
	"context"
	"errors"
	"maps"
	"testing"
	"time"

	"github.com/sberz/ephemeral-envs/internal/probe"
)

//nolint:gocognit // Comprehensive table-driven test intentionally covers many ResolveProbes branches.
func TestEnvironmentResolveProbesFilterAndMetadata(t *testing.T) {
	t.Parallel()

	baseEnv := Environment{
		Name:      "test",
		Namespace: "env-test",
		CreatedAt: time.Unix(1700000000, 0).UTC(),
		URL: map[string]string{
			"app": "https://example.test",
		},
		StatusChecks: map[string]probe.Probe[bool]{
			"healthy": probe.NewStaticProbe(true),
			"ready":   probe.NewStaticProbe(false),
		},
		MetaProbes: map[string]probe.MetadataProbe{
			"owner": probe.WrapProbe(probe.NewStaticProbe("team-core")),
		},
	}

	//nolint:govet // Keep table field order for readability.
	tests := []struct {
		env            Environment
		includeMeta    bool
		filter         map[string]bool
		wantStatus     map[string]bool
		wantStatusKeys map[string]bool
		wantMeta       map[string]any
		wantErr        bool
		name           string
	}{
		{
			name:        "filter and metadata",
			env:         baseEnv,
			includeMeta: true,
			filter:      map[string]bool{"healthy": true},
			wantStatus:  map[string]bool{"healthy": true},
			wantStatusKeys: map[string]bool{
				"healthy": true,
			},
			wantMeta: map[string]any{"owner": "team-core"},
		},
		{
			name:           "resolve all status no metadata",
			env:            baseEnv,
			includeMeta:    false,
			filter:         nil,
			wantStatus:     map[string]bool{"healthy": true, "ready": false},
			wantStatusKeys: map[string]bool{"healthy": true, "ready": true},
			wantMeta:       nil,
		},
		{
			name:        "false filter values are skipped",
			env:         baseEnv,
			includeMeta: true,
			filter:      map[string]bool{"healthy": false, "ready": true},
			wantStatus:  map[string]bool{"ready": false},
			wantStatusKeys: map[string]bool{
				"ready": true,
			},
			wantMeta: map[string]any{"owner": "team-core"},
		},
		{
			name: "empty status checks",
			env: Environment{
				Name:         baseEnv.Name,
				Namespace:    baseEnv.Namespace,
				CreatedAt:    baseEnv.CreatedAt,
				URL:          baseEnv.URL,
				StatusChecks: map[string]probe.Probe[bool]{},
				MetaProbes:   baseEnv.MetaProbes,
			},
			includeMeta:    true,
			filter:         nil,
			wantStatus:     map[string]bool{},
			wantStatusKeys: map[string]bool{},
			wantMeta:       map[string]any{"owner": "team-core"},
		},
		{
			name: "nil status checks",
			env: Environment{
				Name:       baseEnv.Name,
				Namespace:  baseEnv.Namespace,
				CreatedAt:  baseEnv.CreatedAt,
				URL:        baseEnv.URL,
				MetaProbes: baseEnv.MetaProbes,
			},
			includeMeta:    true,
			filter:         nil,
			wantStatus:     map[string]bool{},
			wantStatusKeys: map[string]bool{},
			wantMeta:       map[string]any{"owner": "team-core"},
		},
		{
			name: "status probe error",
			env: Environment{
				Name:      baseEnv.Name,
				Namespace: baseEnv.Namespace,
				CreatedAt: baseEnv.CreatedAt,
				URL:       baseEnv.URL,
				StatusChecks: map[string]probe.Probe[bool]{
					"healthy": failingBoolProbe{},
				},
				MetaProbes: map[string]probe.MetadataProbe{},
			},
			includeMeta: false,
			wantErr:     true,
		},
		{
			name: "metadata probe error",
			env: Environment{
				Name:      baseEnv.Name,
				Namespace: baseEnv.Namespace,
				CreatedAt: baseEnv.CreatedAt,
				URL:       baseEnv.URL,
				StatusChecks: map[string]probe.Probe[bool]{
					"healthy": probe.NewStaticProbe(true),
				},
				MetaProbes: map[string]probe.MetadataProbe{
					"owner": failingMetadataProbe{},
				},
			},
			includeMeta: true,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res, err := tt.env.ResolveProbes(context.Background(), tt.includeMeta, tt.filter)
			if tt.wantErr {
				if err == nil {
					t.Fatal("ResolveProbes() error = nil, want non-nil")
				}

				if !errors.Is(err, errProbeFailed) {
					t.Fatalf("ResolveProbes() error = %v, want wrapped errProbeFailed", err)
				}

				return
			}

			if err != nil {
				t.Fatalf("ResolveProbes() error = %v", err)
			}

			if !maps.Equal(res.Status, tt.wantStatus) {
				t.Fatalf("status = %#v, want %#v", res.Status, tt.wantStatus)
			}

			if !maps.EqualFunc(res.Meta, tt.wantMeta, func(a any, b any) bool { return a == b }) {
				t.Fatalf("meta = %#v, want %#v", res.Meta, tt.wantMeta)
			}

			if len(res.StatusUpdated) != len(tt.wantStatusKeys) {
				t.Fatalf("statusUpdated len = %d, want %d", len(res.StatusUpdated), len(tt.wantStatusKeys))
			}

			for key := range tt.wantStatusKeys {
				if _, ok := res.StatusUpdated[key]; !ok {
					t.Fatalf("statusUpdated missing key %q in %#v", key, res.StatusUpdated)
				}
			}
		})
	}
}

func TestEnvironmentMatchesStatus(t *testing.T) {
	t.Parallel()

	env := Environment{
		StatusChecks: map[string]probe.Probe[bool]{
			"healthy": probe.NewStaticProbe(true),
			"ready":   probe.NewStaticProbe(false),
		},
	}

	//nolint:govet // Keep table field order for readability.
	tests := []struct {
		state map[string]bool
		want  bool
		name  string
	}{
		{
			name:  "empty filter matches",
			state: map[string]bool{},
			want:  true,
		},
		{
			name:  "exact matching checks",
			state: map[string]bool{"healthy": true, "ready": false},
			want:  true,
		},
		{
			name:  "mismatch check value",
			state: map[string]bool{"ready": true},
			want:  false,
		},
		{
			name:  "missing check required true",
			state: map[string]bool{"missing": true},
			want:  false,
		},
		{
			name:  "missing check required false",
			state: map[string]bool{"missing": false},
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := env.MatchesStatus(context.Background(), tt.state)
			if got != tt.want {
				t.Fatalf("MatchesStatus() = %t, want %t", got, tt.want)
			}
		})
	}
}

var errProbeFailed = errors.New("probe failed")

type failingBoolProbe struct{}

func (f failingBoolProbe) Value(_ context.Context) (bool, error) {
	return false, errProbeFailed
}

func (f failingBoolProbe) LastUpdate() time.Time {
	return time.Time{}
}

type failingMetadataProbe struct{}

func (f failingMetadataProbe) Value(_ context.Context) (any, error) {
	return nil, errProbeFailed
}

func (f failingMetadataProbe) LastUpdate() time.Time {
	return time.Time{}
}
