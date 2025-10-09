package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
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
	mux.Handle("GET /v1/environments", handleListEnvironmentNames(store))
	mux.Handle("GET /v1/environments/{name}/details", handleGetEnvironment(store))

	// Register Middleware for logging
	var handler http.Handler = mux
	handler = middlewarePanicRecovery(handler)
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

func handleHealthCheck() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mustEncodeResponse(w, r, http.StatusOK, map[string]string{
			"status": "ok",
		})
	})
}

func handleListEnvironmentNames(s *store.Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		environments := s.ListEnvironmentNames(r.Context())

		mustEncodeResponse(w, r, http.StatusOK, environments)
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

		mustEncodeResponse(w, r, http.StatusOK, env)
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
