package main

import (
	"context"
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
	s      *store.Store
	checks map[string]probe.Prober[bool]
}

func NewEventHandler(_ context.Context, store *store.Store, checks map[string]probe.Prober[bool]) *EventHandler {
	return &EventHandler{
		s:      store,
		checks: checks,
	}
}

func (c *EventHandler) HandleNamespaceAdd(ctx context.Context, ns *corev1.Namespace) {
	name := ns.Labels[LabelEnvName]

	urls := c.buildURLMap(ctx, ns)
	checks := c.buildStatusChecks(ctx, name, ns)

	err := c.s.AddEnvironment(ctx, store.Environment{
		Name:         name,
		CreatedAt:    ns.GetCreationTimestamp().Time,
		Namespace:    ns.Name,
		URL:          urls,
		StatusChecks: checks,
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

	err := c.s.UpdateEnvironment(ctx, oldName, store.Environment{
		Name:         newName,
		CreatedAt:    newNs.GetCreationTimestamp().Time,
		Namespace:    newNs.Name,
		URL:          urls,
		StatusChecks: checks,
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
