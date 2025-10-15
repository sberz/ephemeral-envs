package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/sberz/ephemeral-envs/internal/store"
)

// statusRecorder is a custom ResponseWriter that captures the status code
// so it can be logged later. It wraps the standard http.ResponseWriter.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

// Implement the WriteHeader method to capture the status code.
func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}

func NewServerHandler(store *store.Store) http.Handler {
	mux := http.NewServeMux()

	mux.Handle("GET /health", handleHealthCheck())
	mux.Handle("GET /v1/environment", handleListEnvironmentNames(store))
	mux.Handle("GET /v1/environment/all", handleGetAllEnvironments(store))
	mux.Handle("GET /v1/environment/{name}", handleGetEnvironment(store))

	// Register Middleware for logging
	var handler http.Handler = mux
	handler = middlewarePanicRecovery(handler)
	handler = middlewareCORS(handler)
	handler = middlewareLogging(handler)

	return handler
}

// middlewareLogging logs all incoming request wit therir method, path, IP and duration.
func middlewareLogging(next http.Handler) http.Handler {

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{w, 200}

		next.ServeHTTP(rec, r)

		duration := time.Since(start)
		slog.InfoContext(r.Context(), "request completed",
			"method", r.Method,
			"path", r.URL.Path,
			"args", r.URL.Query(),
			"remote_addr", r.RemoteAddr,
			"duration_us", duration.Microseconds(),
			"status", rec.status,
		)
	})
}

func middlewarePanicRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func(ctx context.Context) {
			if err := recover(); err != nil {
				slog.ErrorContext(ctx, "panic recovered", "error", err, "stack", string(debug.Stack()))
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}(r.Context())
		next.ServeHTTP(w, r)
	})
}

func middlewareCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// This server doesn't require Authentication, so sefelisted CORS will do
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Max-Age", "86400") // 24 hours

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func handleHealthCheck() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mustEncodeResponse(w, r, http.StatusOK, map[string]string{
			"status": "ok",
		})
	})
}

func handleListEnvironmentNames(s *store.Store) http.Handler {
	type response struct {
		Environments []string `json:"environments"`
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		filterNamespace := r.URL.Query().Get("namespace")
		filterStatus := parseStatusFilter(r, "status")

		slog.InfoContext(r.Context(), "listing environments", "namespace", filterNamespace, "status", filterStatus)

		envs := []string{}

		switch {
		case filterNamespace != "":
			env, err := s.GetEnvironmentByNamespace(r.Context(), filterNamespace)
			if err != nil && !errors.Is(err, store.ErrEnvironmentNotFound) {
				slog.ErrorContext(r.Context(), "failed to get environments by namespace", "error", err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}

			if len(filterStatus) == 0 || env.MatchesStatus(r.Context(), filterStatus) {
				envs = []string{env.Name}
			}
		case len(filterStatus) > 0:
			envs = s.GetEnvironmentNamesWithState(r.Context(), filterStatus)
		default:
			envs = s.ListEnvironmentNames(r.Context())
		}

		mustEncodeResponse(w, r, http.StatusOK, response{Environments: envs})
	})
}

func handleGetEnvironment(s *store.Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")

		env, err := s.GetEnvironment(r.Context(), name)
		if err != nil {
			if errors.Is(err, store.ErrEnvironmentNotFound) {
				http.Error(w, "Environment Not Found", http.StatusNotFound)
			} else {
				slog.ErrorContext(r.Context(), "failed to get environment", "error", err, "name", name)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
			return
		}

		es, err := env.ResolveProbes(r.Context(), nil)
		if err != nil {
			slog.ErrorContext(r.Context(), "failed to resolve probes for environment", "error", err, "name", name)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		mustEncodeResponse(w, r, http.StatusOK, es)
	})
}

func handleGetAllEnvironments(s *store.Store) http.Handler {
	type response struct {
		Environments []store.EnvironmentResponse `json:"environments"`
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		includeStatus := parseStatusFilter(r, "withStatus")
		envs := s.GetAllEnvironments(r.Context())
		res := make([]store.EnvironmentResponse, 0, len(envs))

		for _, env := range envs {
			es, err := env.ResolveProbes(r.Context(), includeStatus)
			if err != nil {
				slog.ErrorContext(r.Context(), "failed to resolve probes for environment", "error", err, "name", env.Name)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			res = append(res, es)
		}

		mustEncodeResponse(w, r, http.StatusOK, response{Environments: res})
	})
}

func encodeResponse[T any](w http.ResponseWriter, _ *http.Request, status int, data T) error {
	// Encode the response data as JSON so errors can still be handled gracefully
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal response data: %w", err)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, err = w.Write(jsonData)
	if err != nil {
		return fmt.Errorf("failed to write response: %w", err)
	}
	return nil
}

func mustEncodeResponse[T any](w http.ResponseWriter, r *http.Request, status int, data T) {
	if err := encodeResponse(w, r, status, data); err != nil {
		slog.ErrorContext(r.Context(), "failed to encode response", "error", err)
		panic(fmt.Errorf("mustEncodeResponse failed: %w", err))
	}
}

func parseStatusFilter(r *http.Request, param string) map[string]bool {
	query := strings.Join(r.URL.Query()[param], ",")
	filter := make(map[string]bool)

	if query == "" {
		return filter
	}

	for _, f := range strings.Split(query, ",") {
		if strings.HasPrefix(f, "!") {
			filter[strings.TrimPrefix(f, "!")] = false
		} else {
			filter[f] = true
		}
	}
	return filter
}
