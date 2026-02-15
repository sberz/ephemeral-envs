package store

import (
	"errors"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/sberz/ephemeral-envs/internal/probe"
)

func TestStoreGetEnvironmentNamesWithStateSorted(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := NewStore()

	envs := []Environment{
		newTestEnvironment("beta", "env-beta", map[string]bool{"healthy": false}),
		newTestEnvironment("alpha", "env-alpha", map[string]bool{"healthy": true}),
		newTestEnvironment("gamma", "env-gamma", map[string]bool{"healthy": true}),
	}

	for _, env := range envs {
		if err := s.AddEnvironment(ctx, env); err != nil {
			t.Fatalf("AddEnvironment() error = %v", err)
		}
	}

	got := s.GetEnvironmentNamesWithState(ctx, map[string]bool{"healthy": true})
	want := []string{"alpha", "gamma"}

	if !slices.Equal(got, want) {
		t.Fatalf("GetEnvironmentNamesWithState() = %#v, want %#v", got, want)
	}
}

func TestStoreUpdateEnvironmentImmutableChangeReadds(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := NewStore()

	createdAt := time.Unix(1700000000, 0).UTC()
	oldEnv := newTestEnvironment("old", "env-old", map[string]bool{"healthy": true})
	oldEnv.CreatedAt = createdAt

	if err := s.AddEnvironment(ctx, oldEnv); err != nil {
		t.Fatalf("AddEnvironment() error = %v", err)
	}

	newEnv := newTestEnvironment("new", "env-new", map[string]bool{"healthy": false})
	newEnv.CreatedAt = createdAt.Add(time.Minute)

	if err := s.UpdateEnvironment(ctx, "old", newEnv); err != nil {
		t.Fatalf("UpdateEnvironment() error = %v", err)
	}

	_, err := s.GetEnvironment(ctx, "old")
	if !errors.Is(err, ErrEnvironmentNotFound) {
		t.Fatalf("GetEnvironment(old) error = %v, want ErrEnvironmentNotFound", err)
	}

	got, err := s.GetEnvironment(ctx, "new")
	if err != nil {
		t.Fatalf("GetEnvironment(new) error = %v", err)
	}

	if got.Name != "new" || got.Namespace != "env-new" {
		t.Fatalf("updated env = %#v, want name=new namespace=env-new", got)
	}
}

func newTestEnvironment(name string, namespace string, checks map[string]bool) Environment {
	statusChecks := make(map[string]probe.Probe[bool], len(checks))
	for checkName, value := range checks {
		statusChecks[checkName] = probe.NewStaticProbe(value)
	}

	return Environment{
		Name:         name,
		Namespace:    namespace,
		CreatedAt:    time.Unix(1700000000, 0).UTC(),
		URL:          map[string]string{"app": "https://example.test/" + name},
		StatusChecks: statusChecks,
		MetaProbes: map[string]probe.MetadataProbe{
			"owner": probe.WrapProbe(probe.NewStaticProbe("team-platform")),
		},
	}
}

func TestEnvironmentIsValid(t *testing.T) {
	t.Parallel()

	env := newTestEnvironment("test", "env-test", map[string]bool{"healthy": true})
	problems := env.IsValid()

	if len(problems) != 0 {
		t.Fatalf("IsValid() = %#v, want no problems", problems)
	}
}

func TestEnvironmentIsValidInvalid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		env  *Environment
		want map[string]string
		name string
	}{
		{
			name: "nil receiver",
			env:  nil,
			want: map[string]string{"environment": invalidNil},
		},
		{
			name: "missing required fields",
			env: &Environment{
				URL:          nil,
				StatusChecks: nil,
				MetaProbes:   nil,
			},
			want: map[string]string{
				"name":         invalidEmpty,
				"createdAt":    invalidZero,
				"namespace":    invalidEmpty,
				"url":          invalidNil,
				"statusChecks": invalidNil,
				"metadata":     invalidNil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.env.IsValid()
			if !maps.Equal(got, tt.want) {
				t.Fatalf("IsValid() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestStoreCRUDAndListing(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := NewStore()

	envA := newTestEnvironment("b", "env-b", map[string]bool{"healthy": true})
	envB := newTestEnvironment("a", "env-a", map[string]bool{"healthy": false})

	if err := s.AddEnvironment(ctx, envA); err != nil {
		t.Fatalf("AddEnvironment(envA) error = %v", err)
	}
	if err := s.AddEnvironment(ctx, envB); err != nil {
		t.Fatalf("AddEnvironment(envB) error = %v", err)
	}

	if got := s.GetEnvironmentCount(ctx); got != 2 {
		t.Fatalf("GetEnvironmentCount() = %d, want 2", got)
	}

	names := s.ListEnvironmentNames(ctx)
	if !slices.Equal(names, []string{"a", "b"}) {
		t.Fatalf("ListEnvironmentNames() = %#v, want [a b]", names)
	}

	all := s.GetAllEnvironments(ctx)
	if len(all) != 2 || all[0].Name != "a" || all[1].Name != "b" {
		t.Fatalf("GetAllEnvironments() = %#v, want sorted by name [a b]", all)
	}

	byNS, err := s.GetEnvironmentByNamespace(ctx, "env-a")
	if err != nil {
		t.Fatalf("GetEnvironmentByNamespace(env-a) error = %v", err)
	}
	if byNS.Name != "a" {
		t.Fatalf("GetEnvironmentByNamespace(env-a).Name = %q, want %q", byNS.Name, "a")
	}

	if err := s.DeleteEnvironment(ctx, "a"); err != nil {
		t.Fatalf("DeleteEnvironment(a) error = %v", err)
	}

	if got := s.GetEnvironmentCount(ctx); got != 1 {
		t.Fatalf("GetEnvironmentCount() after delete = %d, want 1", got)
	}

	if _, err := s.GetEnvironment(ctx, "a"); !errors.Is(err, ErrEnvironmentNotFound) {
		t.Fatalf("GetEnvironment(a) error = %v, want ErrEnvironmentNotFound", err)
	}

	if err := s.DeleteEnvironment(ctx, "a"); !errors.Is(err, ErrEnvironmentNotFound) {
		t.Fatalf("DeleteEnvironment(a) second time error = %v, want ErrEnvironmentNotFound", err)
	}
}

func TestStoreUpdateEnvironmentAddsWhenMissing(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := NewStore()
	env := newTestEnvironment("new", "env-new", map[string]bool{"healthy": true})

	if err := s.UpdateEnvironment(ctx, "missing", env); err != nil {
		t.Fatalf("UpdateEnvironment(missing, env) error = %v", err)
	}

	got, err := s.GetEnvironment(ctx, "new")
	if err != nil {
		t.Fatalf("GetEnvironment(new) error = %v", err)
	}

	if got.Namespace != "env-new" {
		t.Fatalf("GetEnvironment(new).Namespace = %q, want %q", got.Namespace, "env-new")
	}
}
