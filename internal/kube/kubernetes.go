package kube

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
)

var ErrInformerCacheSyncFailed = errors.New("failed to sync informer cache")

// GetClient return a configured Kubernetes client. It uses the kube config file set in the KUBECONFIG environment variable if it is set, otherwise it uses in-cluster configuration.
func GetClient() (*kubernetes.Clientset, error) {
	kubeconfig := os.Getenv("KUBECONFIG")
	var config *rest.Config
	var err error

	if kubeconfig != "" {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		config, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes clientset: %w", err)
	}

	return clientset, nil
}

// WatchNamespaceEvents registers event handlers for namespace events in the Kubernetes cluster.
// Only namespaces matching the provided label selector will trigger the handlers.
// onAdd, onUpdate, onDelete are called with *corev1.Namespace as argument.
func WatchNamespaceEvents(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	labelSelector string,
	onAdd func(ctx context.Context, ns *corev1.Namespace),
	onUpdate func(ctx context.Context, oldNs, newNs *corev1.Namespace),
	onDelete func(ctx context.Context, ns *corev1.Namespace),
) error {
	opts := informers.WithTweakListOptions(func(lo *metav1.ListOptions) {
		lo.LabelSelector = labelSelector
	})

	factory := informers.NewSharedInformerFactoryWithOptions(clientset, 10*time.Minute, opts)
	nsInformer := factory.Core().V1().Namespaces().Informer()

	_, err := nsInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			ns := toNamespace(ctx, obj)
			if ns == nil {
				return
			}
			slog.DebugContext(ctx, "namespace added", "name", ns.Name, "labels", ns.Labels)

			if onAdd != nil {
				onAdd(ctx, ns)
			} else {
				slog.WarnContext(ctx, "onAdd handler is nil, skipping add event", "name", ns.Name)
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldNs := toNamespace(ctx, oldObj)
			if oldNs == nil {
				return
			}
			newNs := toNamespace(ctx, newObj)
			if newNs == nil {
				return
			}
			slog.DebugContext(ctx, "namespace updated", "name", newNs.Name, "oldLabels", oldNs.Labels, "newLabels", newNs.Labels)

			if onUpdate != nil {
				onUpdate(ctx, oldNs, newNs)
			} else {
				slog.WarnContext(ctx, "onUpdate handler is nil, skipping update event", "name", newNs.Name)
			}
		},
		DeleteFunc: func(obj interface{}) {
			ns := toNamespace(ctx, obj)
			if ns == nil {
				return
			}
			slog.DebugContext(ctx, "namespace deleted", "name", ns.Name, "labels", ns.Labels)

			if onDelete != nil {
				onDelete(ctx, ns)
			} else {
				slog.WarnContext(ctx, "onDelete handler is nil, skipping delete event", "name", ns.Name)
			}
		},
	})
	if err != nil {
		return fmt.Errorf("failed to add event handler to namespace informer: %w", err)
	}

	factory.Start(ctx.Done())
	for informerType, synced := range factory.WaitForCacheSync(ctx.Done()) {
		if !synced {
			return fmt.Errorf("%w: %v", ErrInformerCacheSyncFailed, informerType)
		}
	}

	return nil
}

// toNamespace converts the object from the event handler to a *corev1.Namespace.
func toNamespace(ctx context.Context, obj interface{}) *corev1.Namespace {
	if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		obj = tombstone.Obj
	}

	if tombstone, ok := obj.(*cache.DeletedFinalStateUnknown); ok && tombstone != nil {
		obj = tombstone.Obj
	}

	ns, ok := obj.(*corev1.Namespace)
	if !ok || ns == nil {
		slog.ErrorContext(ctx, "received object is not a Namespace", "objectType", fmt.Sprintf("%T", obj))
		return nil
	}
	return ns
}
