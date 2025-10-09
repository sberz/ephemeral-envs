package kube

import (
	"context"
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
	onAdd func(ns *corev1.Namespace),
	onUpdate func(oldNs, newNs *corev1.Namespace),
	onDelete func(ns *corev1.Namespace),
) error {
	opts := informers.WithTweakListOptions(func(lo *metav1.ListOptions) {
		lo.LabelSelector = labelSelector
	})

	factory := informers.NewSharedInformerFactoryWithOptions(clientset, time.Minute*10, opts)
	nsInformer := factory.Core().V1().Namespaces().Informer()

	nsInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			ns := obj.(*corev1.Namespace)
			slog.DebugContext(ctx, "Namespace added", "name", ns.Name, "labels", ns.Labels)

			if onAdd != nil {
				onAdd(ns)
			} else {
				slog.WarnContext(ctx, "onAdd handler is nil, skipping add event", "name", ns.Name)
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldNs := oldObj.(*corev1.Namespace)
			newNs := newObj.(*corev1.Namespace)
			slog.DebugContext(ctx, "Namespace updated", "name", newNs.Name, "oldLabels", oldNs.Labels, "newLabels", newNs.Labels)

			if onUpdate != nil {
				onUpdate(oldNs, newNs)
			} else {
				slog.WarnContext(ctx, "onUpdate handler is nil, skipping update event", "name", newNs.Name)
			}
		},
		DeleteFunc: func(obj interface{}) {
			ns := obj.(*corev1.Namespace)
			slog.DebugContext(ctx, "Namespace deleted", "name", ns.Name, "labels", ns.Labels)

			if onDelete != nil {
				onDelete(ns)
			} else {
				slog.WarnContext(ctx, "onDelete handler is nil, skipping delete event", "name", ns.Name)
			}
		},
	})

	factory.Start(ctx.Done())
	factory.WaitForCacheSync(ctx.Done())

	return nil
}
