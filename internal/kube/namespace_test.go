package kube

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
)

func TestToNamespace(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ns := &corev1.Namespace{}
	ns.Name = "env-test"

	tests := []struct {
		want *corev1.Namespace
		obj  any
		name string
	}{
		{name: "namespace object", obj: ns, want: ns},
		{name: "deleted tombstone value", obj: cache.DeletedFinalStateUnknown{Obj: ns}, want: ns},
		{name: "deleted tombstone pointer", obj: &cache.DeletedFinalStateUnknown{Obj: ns}, want: ns},
		{name: "deleted tombstone nil pointer", obj: (*cache.DeletedFinalStateUnknown)(nil), want: nil},
		{name: "invalid object", obj: "nope", want: nil},
		{name: "tombstone invalid inner", obj: cache.DeletedFinalStateUnknown{Obj: 42}, want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := toNamespace(ctx, tt.obj)
			if got != tt.want {
				t.Fatalf("toNamespace() = %v, want %v", got, tt.want)
			}
		})
	}
}
