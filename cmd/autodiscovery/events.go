package main

import (
	"context"
	"log/slog"
	"strings"

	"github.com/sberz/ephemeral-envs/internal/store"
	corev1 "k8s.io/api/core/v1"
)

type EventHandler struct {
	s *store.Store
}

func NewEventHandler(store *store.Store) *EventHandler {
	return &EventHandler{
		s: store,
	}
}

func (c *EventHandler) HandleNamespaceAdd(ctx context.Context, ns *corev1.Namespace) {
	name := ns.Labels[LabelEnvName]

	urls := c.buildURLMap(ctx, ns)

	err := c.s.AddEnvironment(ctx, store.Environment{
		Name:      name,
		CreatedAt: ns.GetCreationTimestamp().Time,
		Namespace: ns.Name,
		URL:       urls,
	})
	if err != nil {
		slog.ErrorContext(ctx, "failed to add environment", "name", name, "error", err)
	}
}

func (c *EventHandler) HandleNamespaceUpdate(ctx context.Context, oldNs, newNs *corev1.Namespace) {

	oldName := oldNs.Labels[LabelEnvName]
	newName := newNs.Labels[LabelEnvName]

	urls := c.buildURLMap(ctx, newNs)

	err := c.s.UpdateEnvironment(ctx, oldName, store.Environment{
		Name:      newName,
		CreatedAt: newNs.GetCreationTimestamp().Time,
		Namespace: newNs.Name,
		URL:       urls,
	})
	if err != nil {
		slog.ErrorContext(ctx, "failed to update environment", "old_name", oldName, "new_name", newName, "error", err)
	}
}

func (c *EventHandler) HandleNamespaceDelete(ctx context.Context, ns *corev1.Namespace) {
	name := ns.Labels[LabelEnvName]

	err := c.s.DeleteEnvironment(ctx, name)
	if err != nil {
		slog.ErrorContext(ctx, "failed to delete environment", "name", name, "error", err)
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
