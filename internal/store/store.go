package store

import (
	"fmt"
	"slices"
	"sync"
	"time"

	"golang.org/x/exp/slog"
)

// Environment is a empheral environment representation.
type Environment struct {
	// Name is the name of the environment provided by the label "envs.sberz.de/name".
	Name string `json:"name"`

	// CreatedAt is the timestamp when the environment was created. This is derived from the namespace creation time.
	CreatedAt time.Time `json:"created_at"`

	// Namespace is the Kubernetes namespace associated with this environment.
	Namespace string `json:"namespace"`

	// URL is a map of URLs associated with the environment, where the key is the URL type (e.g., "web", "api") and the value is the URL string.
	// This allows for multiple URLs to be associated with the environment, such as a web URL and an API URL.
	URL map[string]string `json:"url"`
}

// IsValid checks if the environment is valid. It returns a map of problems if
// any validation fails.
func (e *Environment) IsValid() (problems map[string]string) {
	problems = make(map[string]string)
	if e == nil {
		problems["environment"] = "cannot be nil"
		return problems
	}

	// Name must be non-empty
	if e.Name == "" {
		problems["name"] = "cannot be empty"
	}

	// CreatedAt must be a valid time (not zero)
	if e.CreatedAt.IsZero() {
		problems["created_at"] = "cannot be zero"
	}

	// Namespace must be non-empty
	if e.Namespace == "" {
		problems["namespace"] = "cannot be empty"
	}

	// URL must be not be nil but can be empty
	if e.URL == nil {
		problems["url"] = "cannot be nil"
	} else {
		// If URL is not nil, it can be empty but should not contain empty values
		for k, v := range e.URL {
			if k == "" {
				problems["url_key"] = "cannot be empty"
			}
			if v == "" {
				problems["url_value"] = "cannot be empty"
			}
		}
	}

	return problems
}

// Update updates the environment with the provided values.
func (e *Environment) UpdateEnvironment(env Environment) error {
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
	mu  sync.Mutex
	env map[string]Environment
}

// NewStore creates a new Store instance.
func NewStore() *Store {
	return &Store{
		env: make(map[string]Environment),
	}
}

// AddEnvironment adds a new environment to the store.
func (s *Store) AddEnvironment(env Environment) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.addEnvironment(env)
}

// addEnvironment is a internal method that adds an environment to the store.
// This method is used internally to avoid code duplication in AddEnvironment and UpdateEnvironment.
// It does not lock the store, so it must be called with the store's mutex already held.
func (s *Store) addEnvironment(env Environment) error {
	problems := env.IsValid()
	if len(problems) > 0 {
		return fmt.Errorf("%w: %v", ErrInvalidEnvironment, problems)
	}

	if _, exists := s.env[env.Name]; exists {
		// If the environment already exists, print a warning and overwrite it.
		// Blocking the creation would desynchronize the store with the Kubernetes events.
		slog.Warn("environment with this name already exists, overwriting it",
			"name", env.Name,
			"namespace", env.Namespace,
		)
	}

	s.env[env.Name] = env

	return nil
}

// DeleteEnvironment removes an environment from the store by its name.
func (s *Store) DeleteEnvironment(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.env, name)

	return nil
}

// GetEnvironment retrieves an environment by its name.
func (s *Store) GetEnvironment(name string) (Environment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	env, exists := s.env[name]
	if !exists {
		return Environment{}, fmt.Errorf("%w: %s", ErrEnvironmentNotFound, name)
	}

	return env, nil
}

// GetEnvironmentCount returns the number of environments currently stored.
func (s *Store) GetEnvironmentCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.env)
}

// ListEnvironmentNames returns a list of all environment names currently stored.
func (s *Store) ListEnvironmentNames() []string {
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
func (s *Store) UpdateEnvironment(name string, env Environment) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	current, exists := s.env[name]
	if !exists {
		// If the environment does not exist, we try to add it
		return s.addEnvironment(env)
	}

	err := current.UpdateEnvironment(env)
	if err != nil {
		return fmt.Errorf("failed to update environment %s: %w", name, err)
	}

	// If the new name is different, we need to replace the environment in the store
	if env.Name != name {
		// AddEnvironment will check for name conflicts
		err := s.addEnvironment(current)
		if err != nil {
			return fmt.Errorf("failed to rename environment %s to %s: %w", name, env.Name, err)
		}

		delete(s.env, name)
	}

	s.env[current.Name] = current
	return nil
}
