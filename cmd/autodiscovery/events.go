package main

import (
	"log/slog"

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

func (c *EventHandler) HandleNamespaceAdd(ns *corev1.Namespace) {
	name := ns.Labels[LabelEnvName]

	err := c.s.AddEnvironment(store.Environment{
		Name:      name,
		CreatedAt: ns.GetCreationTimestamp().Time,
		Namespace: ns.Name,
	})
	if err != nil {
		slog.Error("failed to add environment", "name", name, "error", err)
	}
}

func (c *EventHandler) HandleNamespaceUpdate(oldNs, newNs *corev1.Namespace) {

	oldName := oldNs.Labels[LabelEnvName]
	newName := newNs.Labels[LabelEnvName]

	err := c.s.RenameEnvironment(oldName, newName)
	if err != nil {
		slog.Error("failed to rename environment", "old_name", oldName, "new_name", newName, "error", err)
	}
}

func (c *EventHandler) HandleNamespaceDelete(ns *corev1.Namespace) {
	name := ns.Labels[LabelEnvName]

	err := c.s.DeleteEnvironment(name)
	if err != nil {
		slog.Error("failed to delete environment", "name", name, "error", err)
	}
}
