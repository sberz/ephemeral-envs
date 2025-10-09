package store

import (
	"fmt"
	"sync"
	"time"
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
	URL map[string]string `json:"url,omitempty"`
}

var (
	ErrNameConflict        = fmt.Errorf("environment with this name already exists")
	ErrInvalidEnvironment  = fmt.Errorf("invalid environment, cannot be added")
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

	if env.Name == "" {
		return fmt.Errorf("%w: environment name cannot be empty", ErrInvalidEnvironment)
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

	return names
}

// RenameEnvironment renames an existing environment.
// If the new name already exists, it will not rename and return an error.
func (s *Store) RenameEnvironment(oldName, newName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if oldName == newName {
		return nil // No change needed
	}

	if _, exists := s.env[newName]; exists {
		return fmt.Errorf("%w: %s", ErrNameConflict, newName)
	}

	env, exists := s.env[oldName]
	if !exists {
		return fmt.Errorf("%w: %s", ErrEnvironmentNotFound, oldName)
	}

	delete(s.env, oldName)
	env.Name = newName
	s.env[newName] = env

	return nil
}
