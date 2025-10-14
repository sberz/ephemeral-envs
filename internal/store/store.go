package store

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	ErrInvalidEnvironment    = fmt.Errorf("invalid environment")
	ErrEnvironmentNotFound   = fmt.Errorf("environment not found")
	ErrImmutableFieldChanged = fmt.Errorf("immutable field changed")
)

var envInfo = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "env_autodiscovery_environment_info",
	Help: "Information about the discovered environments",
}, []string{"name", "namespace"})

// Store manages ephemeral environments.
// It provides methods to add, update, delete, and retrieve environments.
type Store struct {
	env map[string]Environment
	mu  sync.RWMutex
}

// NewStore creates a new Store instance.
func NewStore() *Store {
	return &Store{
		env: make(map[string]Environment),
	}
}

// addEnvironment is a internal method that adds an environment to the store.
// This method is used internally to avoid code duplication in AddEnvironment and UpdateEnvironment.
// It does not lock the store, so it must be called with the store's mutex already held.
func (s *Store) addEnvironment(ctx context.Context, env Environment) error {
	problems := env.IsValid()
	if len(problems) > 0 {
		return fmt.Errorf("%w: %v", ErrInvalidEnvironment, problems)
	}

	if oldEnv, exists := s.env[env.Name]; exists {
		// If the environment already exists, print a warning and overwrite it.
		// Blocking the creation would desynchronize the store with the Kubernetes events.
		slog.WarnContext(ctx, "environment with this name already exists, overwriting it",
			"name", env.Name,
			"old_namespace", oldEnv.Namespace,
			"new_namespace", env.Namespace,
		)

		err := s.deleteEnvironment(ctx, env.Name)
		if err != nil {
			return fmt.Errorf("could not remove previous env: %w", err)
		}
	}

	s.env[env.Name] = env
	envInfo.WithLabelValues(env.Name, env.Namespace).Set(1)

	return nil
}

// deleteEnvironment is a internal method to remove a environment.
// It does not lock the store, so it must be called with the store's mutex already held.
func (s *Store) deleteEnvironment(ctx context.Context, name string) error {
	env, exists := s.env[name]
	if !exists {
		return fmt.Errorf("%w: %s", ErrEnvironmentNotFound, name)
	}

	for k, v := range env.StatusChecks {
		if v != nil {
			err := v.Destroy(ctx)
			if err != nil {
				slog.ErrorContext(ctx, "failed to destroy status check", "check", k, "error", err)
			}
		}
	}

	delete(s.env, name)
	// Clean up the metric
	envInfo.DeleteLabelValues(env.Name, env.Namespace)

	return nil
}

// AddEnvironment adds a new environment to the store.
func (s *Store) AddEnvironment(ctx context.Context, env Environment) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.addEnvironment(ctx, env)
}

// DeleteEnvironment removes an environment from the store by its name.
func (s *Store) DeleteEnvironment(ctx context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.deleteEnvironment(ctx, name)
}

// GetEnvironment retrieves an environment by its name.
func (s *Store) GetEnvironment(_ context.Context, name string) (Environment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	env, exists := s.env[name]
	if !exists {
		return Environment{}, fmt.Errorf("%w: %s", ErrEnvironmentNotFound, name)
	}

	return env, nil
}

// GetAllEnvironments retrieves all environments in the store.
func (s *Store) GetAllEnvironments(_ context.Context) []Environment {
	s.mu.RLock()
	defer s.mu.RUnlock()

	envs := make([]Environment, 0, len(s.env))
	for _, env := range s.env {
		envs = append(envs, env)
	}

	return envs
}

// GetEnvironmentByNamespace retrieves an environment by its namespace.
func (s *Store) GetEnvironmentByNamespace(_ context.Context, namespace string) (Environment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, env := range s.env {
		if env.Namespace == namespace {
			return env, nil
		}
	}

	return Environment{}, fmt.Errorf("%w: namespace %s", ErrEnvironmentNotFound, namespace)
}

// GetEnvironmentNamesWithState returns a list of environment names that match the provided status check states.
func (s *Store) GetEnvironmentNamesWithState(ctx context.Context, state map[string]bool) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	envs := []string{}
	for name, env := range s.env {
		if env.MatchesStatus(ctx, state) {
			envs = append(envs, name)
		}

		// Sort the names for consistent ordering
		slices.Sort(envs)
	}

	return envs
}

// GetEnvironmentCount returns the number of environments currently stored.
func (s *Store) GetEnvironmentCount(_ context.Context) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.env)
}

// ListEnvironmentNames returns a list of all environment names currently stored.
func (s *Store) ListEnvironmentNames(_ context.Context) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	names := make([]string, 0, len(s.env))
	for name := range s.env {
		names = append(names, name)
	}

	// Sort the names for consistent ordering
	slices.Sort(names)

	return names
}

// UpdateEnvironment updates an existing environment.
// name must be the name of the environment to update. A new name can be provided
// in the env parameter to rename the environment.
func (s *Store) UpdateEnvironment(ctx context.Context, name string, env Environment) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	current, exists := s.env[name]
	if !exists {
		// If the environment does not exist, we try to add it
		return s.addEnvironment(ctx, env)
	}

	err := current.UpdateEnvironment(ctx, env)
	switch {
	case err == nil:
		s.env[env.Name] = current
	case errors.Is(err, ErrImmutableFieldChanged):
		// Immutable fields were changed, we need to delete and re-add the environment
		slog.InfoContext(ctx, "immutable fields changed, re-adding environment",
			"old_name", name,
			"new_name", env.Name,
			"namespace", env.Namespace,
		)

		err = s.deleteEnvironment(ctx, name)
		if err != nil {
			return fmt.Errorf("could not remove previous env: %w", err)
		}
		return s.addEnvironment(ctx, env)
	default:
		return fmt.Errorf("failed to update environment %s: %w", name, err)
	}

	return nil
}
