package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sberz/ephemeral-envs/internal/kube"
	"github.com/sberz/ephemeral-envs/internal/store"
)

const (
	LabelEnvName = "envs.sberz.de/name"

	AnnotationEnvURLPrefix         = "url.envs.sberz.de/"
	AnnotationEnvStatusCheckPrefix = "status.envs.sberz.de/"
	AnnotationEnvMetadataPrefix    = "metadata.envs.sberz.de/"
)

var logLevel = &slog.LevelVar{}

var (
	envTotalOpt = prometheus.GaugeOpts{
		Name: "ephemeralenv_environments",
		Help: "Total number of discovered environments",
	}
)

func main() {
	ctx := context.Background()

	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		slog.ErrorContext(ctx, "failed to run autodiscovery", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	logger := slog.New(slog.NewJSONHandler(stdout, &slog.HandlerOptions{
		AddSource: false,
		Level:     logLevel,
	}))
	slog.SetDefault(logger)

	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cfg, err := parseConfig(args, stderr)
	if err != nil {
		return fmt.Errorf("can not load config: %w", err)
	}

	logLevel.Set(cfg.LogLevel)

	slog.DebugContext(ctx, "starting autodiscovery service", "args", args)

	slog.DebugContext(ctx, "setting up Kubernetes client")
	clientset, err := kube.GetClient()
	if err != nil {
		return fmt.Errorf("failed to get Kubernetes client: %w", err)
	}

	envStore := store.NewStore()

	promauto.NewGaugeFunc(envTotalOpt, func() float64 {
		return float64(envStore.GetEnvironmentCount(ctx))
	})

	statusChecks, metadataProbers, err := setupProbers(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to set up probers: %w", err)
	}

	ignitionProvider, err := setupIgnitionProvider(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to set up ignition provider: %w", err)
	}

	slog.DebugContext(ctx, "watching namespace events")
	controller := NewEventHandler(ctx, envStore, statusChecks, metadataProbers)
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

	slog.InfoContext(ctx, "initial sync complete, waiting for events", "env_count", envStore.GetEnvironmentCount(ctx))

	// Start the HTTP server
	slog.DebugContext(ctx, "starting HTTP server", "port", cfg.Port)
	errLogger := slog.NewLogLogger(logger.Handler(), slog.LevelError)

	server := http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      NewServerHandler(envStore, ignitionProvider),
		ErrorLog:     errLogger,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	serverErrs := make(chan error, 2)

	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrs <- fmt.Errorf("HTTP server failed: %w", err)
		}
	}()

	var metricsServer *http.Server
	if cfg.MetricsPort != 0 {
		slog.DebugContext(ctx, "starting metrics server", "port", cfg.MetricsPort)

		metricsMux := http.NewServeMux()
		metricsMux.Handle("/metrics", promhttp.Handler())

		metricsServer = &http.Server{
			Addr:         fmt.Sprintf(":%d", cfg.MetricsPort),
			Handler:      metricsMux,
			ErrorLog:     errLogger,
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 10 * time.Second,
		}

		go func() {
			if err := metricsServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				serverErrs <- fmt.Errorf("metrics server failed: %w", err)
			}
		}()
	}

	slog.InfoContext(ctx, "autodiscovery service started", "address", server.Addr)

	select {
	case <-ctx.Done():
		// shutdown below
	case err := <-serverErrs:
		return err
	}

	slog.InfoContext(ctx, "shutting down server gracefully")
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("failed to shut down server gracefully: %w", err)
	}

	if metricsServer != nil {
		if err := metricsServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("failed to shut down metrics server gracefully: %w", err)
		}
	}

	return nil
}
