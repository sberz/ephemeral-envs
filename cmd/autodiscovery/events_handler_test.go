package main

import (
	"errors"
	"testing"
	"time"

	"github.com/sberz/ephemeral-envs/internal/probe"
	"github.com/sberz/ephemeral-envs/internal/store"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestEventHandlerBuildStatusChecksAnnotationOverridesProber(t *testing.T) {
	t.Parallel()

	promOKProber := &recordingBoolProber{probe: probe.NewStaticProbe(true)}
	extraProber := &recordingBoolProber{probe: probe.NewStaticProbe(true)}

	h := NewEventHandler(t.Context(), store.NewStore(), map[string]probe.Prober[bool]{
		"prom_ok":     promOKProber,
		"from_prober": extraProber,
	}, nil)

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name: "env-a",
		Annotations: map[string]string{
			AnnotationEnvStatusCheckPrefix + "prom_ok": "0",
			AnnotationEnvStatusCheckPrefix + "active":  "1",
		},
	}}

	checks := h.buildStatusChecks(t.Context(), "a", ns)

	promOKVal, err := checks["prom_ok"].Value(t.Context())
	if err != nil {
		t.Fatalf("prom_ok Value() error = %v", err)
	}
	if promOKVal {
		t.Fatalf("prom_ok = %t, want false (annotation should override prober)", promOKVal)
	}

	if promOKProber.calls != 0 {
		t.Fatalf("promOKProber calls = %d, want 0", promOKProber.calls)
	}

	if extraProber.calls != 1 {
		t.Fatalf("extraProber calls = %d, want 1", extraProber.calls)
	}

	extraVal, err := checks["from_prober"].Value(t.Context())
	if err != nil {
		t.Fatalf("from_prober Value() error = %v", err)
	}
	if !extraVal {
		t.Fatalf("from_prober = %t, want true", extraVal)
	}
}

func TestEventHandlerBuildMetadataProbesAnnotationOverridesProber(t *testing.T) {
	t.Parallel()

	ownerProber := &recordingMetadataProber{probe: probe.WrapProbe(probe.NewStaticProbe("team-prober"))}
	extraProber := &recordingMetadataProber{probe: probe.WrapProbe(probe.NewStaticProbe("extra"))}

	h := NewEventHandler(t.Context(), store.NewStore(), nil, map[string]probe.MetadataProber{
		"owner":       ownerProber,
		"from_prober": extraProber,
	})

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name: "env-a",
		Annotations: map[string]string{
			AnnotationEnvMetadataPrefix + "owner": `"team-annotation"`,
		},
	}}

	meta := h.buildMetadataProbes(t.Context(), "a", ns)

	ownerVal, err := meta["owner"].Value(t.Context())
	if err != nil {
		t.Fatalf("owner Value() error = %v", err)
	}
	if ownerVal != "team-annotation" {
		t.Fatalf("owner = %#v, want %#v", ownerVal, "team-annotation")
	}

	if ownerProber.calls != 0 {
		t.Fatalf("ownerProber calls = %d, want 0", ownerProber.calls)
	}

	if extraProber.calls != 1 {
		t.Fatalf("extraProber calls = %d, want 1", extraProber.calls)
	}
}

func TestEventHandlerHandleNamespaceUpdateRenameAndDelete(t *testing.T) {
	t.Parallel()

	s := store.NewStore()
	h := NewEventHandler(t.Context(), s, nil, nil)

	created := time.Unix(1_700_000_000, 0).UTC()
	oldNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name:              "env-a",
		CreationTimestamp: metav1.NewTime(created),
		Labels: map[string]string{
			LabelEnvName: "old",
		},
		Annotations: map[string]string{
			AnnotationEnvURLPrefix + "app": "https://old.example.test",
		},
	}}

	h.HandleNamespaceAdd(t.Context(), oldNS)

	newNS := oldNS.DeepCopy()
	newNS.Labels[LabelEnvName] = "new"
	newNS.Annotations[AnnotationEnvURLPrefix+"app"] = "https://new.example.test"
	h.HandleNamespaceUpdate(t.Context(), oldNS, newNS)

	if _, err := s.GetEnvironment(t.Context(), "old"); !errors.Is(err, store.ErrEnvironmentNotFound) {
		t.Fatalf("GetEnvironment(old) error = %v, want ErrEnvironmentNotFound", err)
	}

	env, err := s.GetEnvironment(t.Context(), "new")
	if err != nil {
		t.Fatalf("GetEnvironment(new) error = %v", err)
	}
	if env.URL["app"] != "https://new.example.test" {
		t.Fatalf("env.URL[app] = %q, want %q", env.URL["app"], "https://new.example.test")
	}

	h.HandleNamespaceDelete(t.Context(), newNS)
	if _, err := s.GetEnvironment(t.Context(), "new"); !errors.Is(err, store.ErrEnvironmentNotFound) {
		t.Fatalf("GetEnvironment(new) after delete error = %v, want ErrEnvironmentNotFound", err)
	}
}

type recordingBoolProber struct {
	probe probe.Probe[bool]
	err   error
	calls int
}

func (r *recordingBoolProber) AddEnvironment(_, _ string) (probe.Probe[bool], error) {
	if r.err != nil {
		return nil, r.err
	}
	r.calls++
	return r.probe, nil
}

type recordingMetadataProber struct {
	probe probe.MetadataProbe
	err   error
	calls int
}

func (r *recordingMetadataProber) AddEnvironment(_, _ string) (probe.MetadataProbe, error) {
	if r.err != nil {
		return nil, r.err
	}
	r.calls++
	return r.probe, nil
}
