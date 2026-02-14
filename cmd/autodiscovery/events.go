package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/sberz/ephemeral-envs/internal/probe"
	"github.com/sberz/ephemeral-envs/internal/store"
	corev1 "k8s.io/api/core/v1"
)

var (
	eventsProcessed = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ephemeralenv_events_processed_total",
		Help: "Total number of processed Kubernetes events",
	}, []string{"event_type", "status"})
)

type EventHandler struct {
	s        *store.Store
	checks   map[string]probe.Prober[bool]
	metadata map[string]probe.MetadataProber
}

func NewEventHandler(_ context.Context, store *store.Store, checks map[string]probe.Prober[bool], metadata map[string]probe.MetadataProber) *EventHandler {
	return &EventHandler{
		s:        store,
		checks:   checks,
		metadata: metadata,
	}
}

func (c *EventHandler) HandleNamespaceAdd(ctx context.Context, ns *corev1.Namespace) {
	name := ns.Labels[LabelEnvName]

	urls := c.buildURLMap(ctx, ns)
	checks := c.buildStatusChecks(ctx, name, ns)
	metadata := c.buildMetadataProbes(ctx, name, ns)

	err := c.s.AddEnvironment(ctx, store.Environment{
		Name:         name,
		CreatedAt:    ns.GetCreationTimestamp().Time,
		Namespace:    ns.Name,
		URL:          urls,
		StatusChecks: checks,
		MetaProbes:   metadata,
	})
	if err != nil {
		slog.ErrorContext(ctx, "failed to add environment", "name", name, "error", err)
		eventsProcessed.WithLabelValues("namespace_add", "error").Inc()
	} else {
		eventsProcessed.WithLabelValues("namespace_add", "success").Inc()
	}
}

func (c *EventHandler) HandleNamespaceUpdate(ctx context.Context, oldNs, newNs *corev1.Namespace) {

	oldName := oldNs.Labels[LabelEnvName]
	newName := newNs.Labels[LabelEnvName]

	urls := c.buildURLMap(ctx, newNs)
	checks := c.buildStatusChecks(ctx, newName, newNs)
	metadata := c.buildMetadataProbes(ctx, newName, newNs)

	err := c.s.UpdateEnvironment(ctx, oldName, store.Environment{
		Name:         newName,
		CreatedAt:    newNs.GetCreationTimestamp().Time,
		Namespace:    newNs.Name,
		URL:          urls,
		StatusChecks: checks,
		MetaProbes:   metadata,
	})
	if err != nil {
		slog.ErrorContext(ctx, "failed to update environment", "old_name", oldName, "new_name", newName, "error", err)
		eventsProcessed.WithLabelValues("namespace_update", "error").Inc()
	} else {
		eventsProcessed.WithLabelValues("namespace_update", "success").Inc()
	}
}

func (c *EventHandler) HandleNamespaceDelete(ctx context.Context, ns *corev1.Namespace) {
	name := ns.Labels[LabelEnvName]

	err := c.s.DeleteEnvironment(ctx, name)
	if err != nil {
		slog.ErrorContext(ctx, "failed to delete environment", "name", name, "error", err)
		eventsProcessed.WithLabelValues("namespace_delete", "error").Inc()
	} else {
		eventsProcessed.WithLabelValues("namespace_delete", "success").Inc()
	}
}

func (c *EventHandler) buildURLMap(ctx context.Context, ns *corev1.Namespace) map[string]string {
	urls := map[string]string{}

	for k, v := range ns.Annotations {
		if !strings.HasPrefix(k, AnnotationEnvURLPrefix) {
			continue
		}

		slog.DebugContext(ctx, "found environment URL annotation", "key", k, "value", v)

		urlName := strings.TrimPrefix(k, AnnotationEnvURLPrefix)
		urls[urlName] = v
	}

	return urls
}

func (c *EventHandler) buildStatusChecks(ctx context.Context, envName string, ns *corev1.Namespace) map[string]probe.Probe[bool] {
	checks := make(map[string]probe.Probe[bool])

	for k, v := range ns.Annotations {
		if !strings.HasPrefix(k, AnnotationEnvStatusCheckPrefix) {
			continue
		}

		slog.DebugContext(ctx, "found environment status check annotation", "key", k, "value", v)

		checkName := strings.TrimPrefix(k, AnnotationEnvStatusCheckPrefix)
		checks[checkName] = probe.NewStaticProbe(v == "true" || v == "1")
	}

	for check, prober := range c.checks {
		if _, exists := checks[check]; exists {
			// Already defined via annotation
			continue
		}

		probe, err := prober.AddEnvironment(envName, ns.Name)
		if err != nil {
			slog.ErrorContext(ctx, "failed to add environment to prober", "check", check, "env_name", envName, "error", err)
			continue
		}
		checks[check] = probe
	}
	return checks
}

func (c *EventHandler) buildMetadataProbes(ctx context.Context, envName string, ns *corev1.Namespace) map[string]probe.MetadataProbe {
	probes := make(map[string]probe.MetadataProbe)

	for k, v := range ns.Annotations {
		if !strings.HasPrefix(k, AnnotationEnvMetadataPrefix) {
			continue
		}

		slog.DebugContext(ctx, "found environment metadata annotation", "key", k, "value", v)
		metaName := strings.TrimPrefix(k, AnnotationEnvMetadataPrefix)
		probes[metaName] = parseMetadataAnnotation(ctx, v)
	}

	for meta, prober := range c.metadata {
		if _, exists := probes[meta]; exists {
			// Already defined via annotation
			continue
		}

		probe, err := prober.AddEnvironment(envName, ns.Name)
		if err != nil {
			slog.ErrorContext(ctx, "failed to add environment to metadata prober", "metadata", meta, "env_name", envName, "error", err)
			continue
		}
		probes[meta] = probe
	}
	return probes
}

// parseMetadataAnnotation tries to parse a metadata annotation as json. If it fails, it falls back to a static string probe.
func parseMetadataAnnotation(ctx context.Context, value string) probe.MetadataProbe {
	// Try to parse as JSON
	var jsonVal any
	err := json.Unmarshal([]byte(value), &jsonVal)
	if err != nil {
		slog.DebugContext(ctx, "failed to parse metadata annotation as JSON, falling back to static string", "value", value, "error", err)
		return probe.WrapProbe(probe.NewStaticProbe(value))
	}

	switch v := jsonVal.(type) {
	case bool:
		return probe.WrapProbe(probe.NewStaticProbe(v))
	case float64:
		return probe.WrapProbe(probe.NewStaticProbe(v))
	case string:
		return probe.WrapProbe(probe.NewStaticProbe(v))
	// Unmarshal will not produce time.Time directly. Timestamps remain strings.
	default:
		slog.DebugContext(ctx, "metadata annotation JSON type is unsupported, falling back to static string", "value", value, "type", fmt.Sprintf("%T", jsonVal))
		return probe.WrapProbe(probe.NewStaticProbe(value))
	}
}
