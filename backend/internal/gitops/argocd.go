package gitops

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
)

// Argo CD mode is a window onto an Argo CD that already runs in the cluster: it
// lists Applications and can ask for a sync. Creating an Application goes
// through the generic YAML apply (Roadmap Phase 25, with a template) rather than
// an endpoint of its own. Everything here runs as the *caller* — the cluster's
// RBAC decides, as for any other page.

var applicationGVR = schema.GroupVersionResource{Group: "argoproj.io", Version: "v1alpha1", Resource: "applications"}

// Application is the part of an Argo CD Application the UI shows.
type Application struct {
	Namespace      string    `json:"namespace"`
	Name           string    `json:"name"`
	Project        string    `json:"project,omitempty"`
	RepoURL        string    `json:"repoURL,omitempty"`
	Path           string    `json:"path,omitempty"`
	TargetRevision string    `json:"targetRevision,omitempty"`
	SyncStatus     string    `json:"syncStatus,omitempty"` // Synced / OutOfSync / Unknown
	Health         string    `json:"health,omitempty"`     // Healthy / Progressing / Degraded ...
	Revision       string    `json:"revision,omitempty"`   // what it last synced to
	Operation      string    `json:"operation,omitempty"`  // phase of the last sync operation
	Message        string    `json:"message,omitempty"`    // its message
	Syncing        bool      `json:"syncing"`              // an operation is pending or running now
	Created        time.Time `json:"created,omitempty"`
}

// DetectArgoCD reports whether the cluster serves Argo CD's Application kind.
func DetectArgoCD(d discovery.DiscoveryInterface) bool {
	list, err := d.ServerResourcesForGroupVersion("argoproj.io/v1alpha1")
	if err != nil || list == nil {
		return false
	}
	for _, r := range list.APIResources {
		if r.Name == "applications" {
			return true
		}
	}
	return false
}

func str(o map[string]any, path ...string) string {
	v, _, _ := unstructured.NestedString(o, path...)
	return v
}

func toApplication(u *unstructured.Unstructured) Application {
	a := Application{
		Namespace: u.GetNamespace(), Name: u.GetName(), Created: u.GetCreationTimestamp().Time,
		Project:    str(u.Object, "spec", "project"),
		SyncStatus: str(u.Object, "status", "sync", "status"),
		Health:     str(u.Object, "status", "health", "status"),
		Revision:   str(u.Object, "status", "sync", "revision"),
		Operation:  str(u.Object, "status", "operationState", "phase"),
		Message:    str(u.Object, "status", "operationState", "message"),
	}
	src := str(u.Object, "spec", "source", "repoURL")
	if src != "" {
		a.RepoURL = src
		a.Path = str(u.Object, "spec", "source", "path")
		a.TargetRevision = str(u.Object, "spec", "source", "targetRevision")
	} else if sources, _, _ := unstructured.NestedSlice(u.Object, "spec", "sources"); len(sources) > 0 {
		if first, ok := sources[0].(map[string]any); ok {
			a.RepoURL = str(first, "repoURL")
			a.Path = str(first, "path")
			a.TargetRevision = str(first, "targetRevision")
		}
	}
	_, hasOp, _ := unstructured.NestedMap(u.Object, "operation")
	a.Syncing = hasOp || a.Operation == "Running"
	return a
}

// ListApplications returns every Application the caller may see, by namespace/name.
func ListApplications(ctx context.Context, dyn dynamic.Interface) ([]Application, error) {
	list, err := dyn.Resource(applicationGVR).Namespace(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]Application, 0, len(list.Items))
	for i := range list.Items {
		out = append(out, toApplication(&list.Items[i]))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// SyncApplication asks Argo CD to sync an Application now, the way `argocd app
// sync` does: by setting the Application's `operation`, which the Argo CD
// application controller picks up and then clears. It refuses if an operation is
// already pending, since merging into a running one would be a guess.
func SyncApplication(ctx context.Context, dyn dynamic.Interface, namespace, name string) error {
	res := dyn.Resource(applicationGVR).Namespace(namespace)
	cur, err := res.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if _, has, _ := unstructured.NestedMap(cur.Object, "operation"); has {
		return fmt.Errorf("%w: a sync of this application is already pending", ErrConflict)
	}
	patch, err := json.Marshal(map[string]any{"operation": map[string]any{
		"initiatedBy": map[string]any{"username": "vinaire"},
		"info":        []any{map[string]any{"name": "Reason", "value": "Triggered from Vinaire"}},
		"sync":        map[string]any{},
	}})
	if err != nil {
		return err
	}
	_, err = res.Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
	if apierrors.IsNotFound(err) {
		return fmt.Errorf("application: %w", ErrNotFound)
	}
	return err
}
