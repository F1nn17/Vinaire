package stream

import (
	"context"
	"log/slog"
	"time"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"github.com/shiraku/vinaire/backend/internal/api"
)

// resyncPeriod controls how often informers replay their full cache as
// synthetic Update events, on top of real watch events. 10 minutes is
// generous for a local dev cluster; it means an occasional harmless burst
// of "modified" events with unchanged data, not a correctness issue.
const resyncPeriod = 10 * time.Minute

// StartWorkloadInformers watches Pods/Deployments/StatefulSets/DaemonSets/
// Jobs across every namespace and broadcasts add/update/delete diffs to hub
// via internal/api's row mappers, so REST responses and stream events use
// identical shaping logic. Blocks until ctx is cancelled.
func StartWorkloadInformers(ctx context.Context, clientset *kubernetes.Clientset, hub *Hub) {
	factory := informers.NewSharedInformerFactory(clientset, resyncPeriod)

	registerInformer(factory.Core().V1().Pods().Informer(), "pods", hub, api.PodRow)
	registerInformer(factory.Apps().V1().Deployments().Informer(), "deployments", hub, api.DeploymentRow)
	registerInformer(factory.Apps().V1().StatefulSets().Informer(), "statefulsets", hub, api.StatefulSetRow)
	registerInformer(factory.Apps().V1().DaemonSets().Informer(), "daemonsets", hub, api.DaemonSetRow)
	registerInformer(factory.Batch().V1().Jobs().Informer(), "jobs", hub, api.JobRow)
	registerInformer(factory.Core().V1().Nodes().Informer(), "nodes", hub, api.NodeRowOf)

	factory.Start(ctx.Done())

	if !cache.WaitForCacheSync(ctx.Done(),
		factory.Core().V1().Pods().Informer().HasSynced,
		factory.Apps().V1().Deployments().Informer().HasSynced,
		factory.Apps().V1().StatefulSets().Informer().HasSynced,
		factory.Apps().V1().DaemonSets().Informer().HasSynced,
		factory.Batch().V1().Jobs().Informer().HasSynced,
		factory.Core().V1().Nodes().Informer().HasSynced,
	) {
		slog.Error("stream: informer caches failed to sync before shutdown")
		return
	}
	slog.Info("stream: informer caches synced, watching for live updates")

	<-ctx.Done()
	slog.Info("stream: informers stopping")
}

// registerInformer wires generic added/modified/deleted handling for one
// resource type onto informer, mapping each object to its row shape R via
// rowOf and broadcasting it on hub tagged with kind. R varies by resource
// kind (api.WorkloadRow for most, api.NodeRow for "nodes") — Event.Row is
// `any`, so any concrete row type satisfies it.
func registerInformer[T any, R any](informer cache.SharedIndexInformer, kind string, hub *Hub, rowOf func(T) R) {
	_, err := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			if v, ok := asResource[T](obj); ok {
				hub.Broadcast(Event{Type: "added", Kind: kind, Row: rowOf(*v)})
			}
		},
		UpdateFunc: func(_, newObj any) {
			if v, ok := asResource[T](newObj); ok {
				hub.Broadcast(Event{Type: "modified", Kind: kind, Row: rowOf(*v)})
			}
		},
		DeleteFunc: func(obj any) {
			if v, ok := asResource[T](obj); ok {
				hub.Broadcast(Event{Type: "deleted", Kind: kind, Row: rowOf(*v)})
			}
		},
	})
	if err != nil {
		slog.Error("stream: failed to register informer handler", "kind", kind, "error", err)
	}
}

// asResource type-asserts obj (or, for deletes, unwraps a
// cache.DeletedFinalStateUnknown tombstone first — the informer uses that
// wrapper when it misses the actual delete event and only knows the last
// observed state).
func asResource[T any](obj any) (*T, bool) {
	if v, ok := obj.(*T); ok {
		return v, true
	}
	tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
	if !ok {
		return nil, false
	}
	v, ok := tombstone.Obj.(*T)
	return v, ok
}
