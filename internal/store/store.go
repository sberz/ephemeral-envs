package store

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"
)

const (
	invalidEmpty = "cannot be empty"
	invalidZero  = "cannot be zero"
	invalidNil   = "cannot be nil"
)

// Environment is a empheral environment representation.
type Environment struct {
	CreatedAt time.Time         `json:"created_at"`
	URL       map[string]string `json:"url"`
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
}

// IsValid checks if the environment is valid. It returns a map of problems if
// any validation fails.
func (e *Environment) IsValid() (problems map[string]string) {
	problems = make(map[string]string)
	if e == nil {
		problems["environment"] = invalidNil
		return problems
	}

	// Name must be non-empty
	if e.Name == "" {
		problems["name"] = invalidEmpty
	}

	// CreatedAt must be a valid time (not zero)
	if e.CreatedAt.IsZero() {
		problems["created_at"] = invalidZero
	}

	// Namespace must be non-empty
	if e.Namespace == "" {
		problems["namespace"] = invalidEmpty
	}

	// URL must be not be nil but can be empty
	if e.URL == nil {
		problems["url"] = invalidNil
	} else {
		// If URL is not nil, it can be empty but should not contain empty values
		for k, v := range e.URL {
			if k == "" {
				problems["url_key"] = invalidEmpty
			}
			if v == "" {
				problems["url_value"] = invalidEmpty
			}
		}
	}

	return problems
}

// Update updates the environment with the provided values.
func (e *Environment) UpdateEnvironment(_ context.Context, env Environment) error {
	if env.Name != "" {
		e.Name = env.Name
	}

	if !env.CreatedAt.IsZero() {
		e.CreatedAt = env.CreatedAt
	}

	if env.Namespace != "" {
		e.Namespace = env.Namespace
	}

	if env.URL != nil {
		e.URL = env.URL
	}

	return nil
}

var (
	ErrInvalidEnvironment  = fmt.Errorf("invalid environment")
	ErrEnvironmentNotFound = fmt.Errorf("environment not found")
)

// Store manages ephemeral environments.
// It provides methods to add, update, delete, and retrieve environments.
type Store struct {
	env map[string]Environment
	mu  sync.Mutex
}

// NewStore creates a new Store instance.
func NewStore() *Store {
	return &Store{
		env: make(map[string]Environment),
	}
}

// AddEnvironment adds a new environment to the store.
func (s *Store) AddEnvironment(ctx context.Context, env Environment) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.addEnvironment(ctx, env)
}

// addEnvironment is a internal method that adds an environment to the store.
// This method is used internally to avoid code duplication in AddEnvironment and UpdateEnvironment.
// It does not lock the store, so it must be called with the store's mutex already held.
func (s *Store) addEnvironment(ctx context.Context, env Environment) error {
	problems := env.IsValid()
	if len(problems) > 0 {
		return fmt.Errorf("%w: %v", ErrInvalidEnvironment, problems)
	}

	if _, exists := s.env[env.Name]; exists {
		// If the environment already exists, print a warning and overwrite it.
		// Blocking the creation would desynchronize the store with the Kubernetes events.
		slog.WarnContext(ctx, "environment with this name already exists, overwriting it",
			"name", env.Name,
			"namespace", env.Namespace,
		)
	}

	s.env[env.Name] = env

	return nil
}

// DeleteEnvironment removes an environment from the store by its name.
func (s *Store) DeleteEnvironment(_ context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.env, name)

	return nil
}

// GetEnvironment retrieves an environment by its name.
func (s *Store) GetEnvironment(_ context.Context, name string) (Environment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	env, exists := s.env[name]
	if !exists {
		return Environment{}, fmt.Errorf("%w: %s", ErrEnvironmentNotFound, name)
	}

	return env, nil
}

// GetEnvironmentCount returns the number of environments currently stored.
func (s *Store) GetEnvironmentCount(_ context.Context) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.env)
}

// ListEnvironmentNames returns a list of all environment names currently stored.
func (s *Store) ListEnvironmentNames(_ context.Context) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

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
	if err != nil {
		return fmt.Errorf("failed to update environment %s: %w", name, err)
	}

	// If the new name is different, we need to replace the environment in the store
	if env.Name != name {
		// AddEnvironment will check for name conflicts
		err := s.addEnvironment(ctx, current)
		if err != nil {
			return fmt.Errorf("failed to rename environment %s to %s: %w", name, env.Name, err)
		}

		delete(s.env, name)
	}

	s.env[current.Name] = current
	return nil
}
