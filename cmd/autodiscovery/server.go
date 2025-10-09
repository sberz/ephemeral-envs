package main

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/sberz/ephemeral-envs/internal/store"
)

func NewServerHandler(store *store.Store) http.Handler {
	mux := http.NewServeMux()

	mux.Handle("GET /v1/environments", handleListEnvironmentNames(store))
	mux.Handle("GET /v1/environments/{name}/details", handleGetEnvironment(store))
	return mux
}

func handleListEnvironmentNames(s *store.Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		environments := s.ListEnvironmentNames(r.Context())

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(environments); err != nil {
			slog.ErrorContext(r.Context(), "failed to encode environments", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
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

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(env); err != nil {
			slog.ErrorContext(r.Context(), "failed to encode environment details", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	})
}
