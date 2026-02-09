package store

import (
	"context"
	"fmt"
	"log/slog"
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
	CreatedAt    time.Time                      `json:"createdAt"`
	URL          map[string]string              `json:"url"`
	StatusChecks map[string]probe.Probe[bool]   `json:"-"`
	MetaProbes   map[string]probe.MetadataProbe `json:"-"`
	Name         string                         `json:"name"`
	Namespace    string                         `json:"namespace"`
}

type EnvironmentResponse struct {
	Status        map[string]bool      `json:"status"`
	StatusUpdated map[string]time.Time `json:"statusUpdatedAt"`
	Meta          map[string]any       `json:"meta,omitempty"`
	Environment
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

	if e.MetaProbes == nil {
		problems["metadata"] = invalidNil
	} else {
		for k, v := range e.MetaProbes {
			if k == "" {
				problems["metadataKey"] = invalidEmpty
			}
			if v == nil {
				problems["metadataValue"] = invalidNil
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

	if env.MetaProbes != nil {
		e.MetaProbes = env.MetaProbes
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

// ResolveProbes resolves the probes for the environment using the provided prober.
// The statusChecks slice contains the names of the probes to resolve. If nil, all
// probes in the environment will be resolved. Empty slice means no probes will be resolved.
func (e *Environment) ResolveProbes(ctx context.Context, includeMeta bool, status map[string]bool) (EnvironmentResponse, error) {
	res := EnvironmentResponse{
		Environment:   *e,
		Status:        make(map[string]bool),
		StatusUpdated: make(map[string]time.Time),
	}

	if includeMeta {
		res.Meta = make(map[string]any)

		if e.MetaProbes != nil {
			for name, probe := range e.MetaProbes {
				val, err := probe.Value(ctx)
				if err != nil {
					slog.ErrorContext(ctx, "failed to get metadata value", "error", err, "name", e.Name, "metadata", name)
					return res, fmt.Errorf("failed to get metadata value for probe %q: %w", name, err)
				}

				res.Meta[name] = val
			}
		}
	}

	if e.StatusChecks != nil && len(e.StatusChecks) == 0 {
		return res, nil
	}

	for name, probe := range e.StatusChecks {
		if status != nil {
			if val, ok := status[name]; !ok || !val {
				// Skip this probe, it's not in the list of probes to resolve or it's set to false
				continue
			}
		}

		val, err := probe.Value(ctx)
		if err != nil {
			slog.ErrorContext(ctx, "failed to get status check value", "error", err, "name", e.Name, "check", name)
			return res, fmt.Errorf("failed to get status check value for probe %q: %w", name, err)
		}

		res.Status[name] = val
		res.StatusUpdated[name] = probe.LastUpdate()
	}

	return res, nil
}
