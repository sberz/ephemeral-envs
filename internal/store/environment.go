package store

import (
	"context"
	"time"

	"github.com/sberz/ephemeral-envs/internal/probe"
)

const (
	invalidEmpty = "cannot be empty"
	invalidZero  = "cannot be zero"
	invalidNil   = "cannot be nil"
)

// Environment is a empheral environment representation.
type Environment struct {
	CreatedAt    time.Time                    `json:"createdAt"`
	URL          map[string]string            `json:"url"`
	StatusChecks map[string]probe.Probe[bool] `json:"-"`
	Name         string                       `json:"name"`
	Namespace    string                       `json:"namespace"`
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
		problems["createdAt"] = invalidZero
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
				problems["urlKey"] = invalidEmpty
			}
			if v == "" {
				problems["urlValue"] = invalidEmpty
			}
		}
	}

	// StatusChecks must be not be nil but can be empty
	if e.StatusChecks == nil {
		problems["statusChecks"] = invalidNil
	} else {
		for k, v := range e.StatusChecks {
			if k == "" {
				problems["statusCheckKey"] = invalidEmpty
			}
			if v == nil {
				problems["statusCheckValue"] = invalidNil
			}
		}
	}

	return problems
}

// Update updates the environment with the provided values.
func (e *Environment) UpdateEnvironment(_ context.Context, env Environment) error {
	if env.Name != "" && env.Name != e.Name {
		return ErrImmutableFieldChanged
	}

	if env.Namespace != "" && env.Namespace != e.Namespace {
		return ErrImmutableFieldChanged
	}

	// As the Namespace is immutable, its property CreatedAt is also immutable.
	if !env.CreatedAt.IsZero() && !env.CreatedAt.Equal(e.CreatedAt) {
		return ErrImmutableFieldChanged
	}

	if env.URL != nil {
		e.URL = env.URL
	}

	if env.StatusChecks != nil {
		e.StatusChecks = env.StatusChecks
	}

	return nil
}

func (e *Environment) MatchesStatus(ctx context.Context, state map[string]bool) bool {
	for check, filterValue := range state {
		probe, exists := e.StatusChecks[check]
		if !exists {
			// Count missing checks as value false
			if filterValue {
				return false
			}
			continue
		}

		// Ignore the error, if the check fails, the value will be false
		val, _ := probe.Value(ctx)
		if val != filterValue {
			return false
		}
	}

	return true
}
