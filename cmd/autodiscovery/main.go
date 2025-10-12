package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sberz/ephemeral-envs/internal/kube"
	"github.com/sberz/ephemeral-envs/internal/store"
)

const (
	LabelEnvName = "envs.sberz.de/name"

	AnnotationEnvURLPrefix = "url.envs.sberz.de/"
)

var logLevel = &slog.LevelVar{}

var (
	envTotalOpt = prometheus.GaugeOpts{
		Name: "env_autodiscovery_environments_total",
		Help: "Total number of discovered environments",
	}
)

type serviceConfig struct {
	MetricsPort int
	Port        int
}

func parseConfig(args []string) (*serviceConfig, error) {
	cfg := &serviceConfig{}
	fs := flag.NewFlagSet("autodiscovery", flag.ContinueOnError)

	fs.Func("log-level", "Set the logging level (DEBUG, INFO, WARN, ERROR)", func(s string) error {
		return logLevel.UnmarshalText([]byte(s))
	})
	fs.IntVar(&cfg.MetricsPort, "metrics-port", 0, "Port to expose Prometheus metrics (0 to disable)")
	fs.IntVar(&cfg.Port, "port", 8080, "Port to run the HTTP server on")

	if err := fs.Parse(args); err != nil {
		return nil, fmt.Errorf("failed to parse args: %w", err)
	}

	return cfg, nil
}

func main() {
	ctx := context.Background()
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: false,
		Level:     logLevel,
	})))

	if err := run(ctx, os.Args[1:]); err != nil {
		slog.ErrorContext(ctx, "failed to run autodiscovery", "error", err)
		os.Exit(1)
	}

	os.Exit(0)
}

func run(ctx context.Context, args []string) error {
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt)
	defer cancel()

	cfg, err := parseConfig(args)
	if err != nil {
		return fmt.Errorf("can not load config: %w", err)
	}

	slog.DebugContext(ctx, "Starting autodiscovery service", "args", args)

	slog.DebugContext(ctx, "Setting up Kubernetes client")
	clientset, err := kube.GetClient()
	if err != nil {
		return fmt.Errorf("failed to get Kubernetes client: %w", err)
	}

	envStore := store.NewStore()

	promauto.NewGaugeFunc(envTotalOpt, func() float64 {
		return float64(envStore.GetEnvironmentCount(ctx))
	})

	slog.DebugContext(ctx, "Watching namespace events")
	controller := NewEventHandler(envStore)
	err = kube.WatchNamespaceEvents(
		ctx,
		clientset,
		LabelEnvName,
		controller.HandleNamespaceAdd,
		controller.HandleNamespaceUpdate,
		controller.HandleNamespaceDelete,
	)
	if err != nil {
		return fmt.Errorf("failed to watch namespace events: %w", err)
	}

	slog.InfoContext(ctx, "Initial sync complete, waiting for events", "env_count", envStore.GetEnvironmentCount(ctx))

	// Start the HTTP server
	slog.DebugContext(ctx, "Starting HTTP server", "port", cfg.Port)

	server := http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      NewServerHandler(envStore),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.ErrorContext(ctx, "HTTP server failed", "error", err)
			os.Exit(1)
		}
	}()

	if cfg.MetricsPort != 0 {
		slog.DebugContext(ctx, "Starting metrics server", "port", cfg.MetricsPort)

		http.Handle("/metrics", promhttp.Handler())
		go func() {
			//nolint:gosec // G114 - not relevant for this internal only server
			if err := http.ListenAndServe(fmt.Sprintf(":%d", cfg.MetricsPort), nil); err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.ErrorContext(ctx, "Metrics server failed", "error", err)
				os.Exit(1)
			}
		}()
	}

	slog.InfoContext(ctx, "Autodiscovery service started", "address", server.Addr)

	// Wait for the server to shut down gracefully
	<-ctx.Done()
	slog.InfoContext(ctx, "Shutting down server gracefully")
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("failed to shut down server gracefully: %w", err)
	}

	return nil
}
