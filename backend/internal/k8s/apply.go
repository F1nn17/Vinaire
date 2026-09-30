package k8s

import (
	"context"
	"errors"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
)

// This file holds Roadmap Phase 25's generic YAML create/update — an
// explicit, deliberate extension to AGENTS.md's write rule, approved by the
// project owner for every resource kind the cluster serves (not an
// allowlist). Authorization is still the caller's own Kubernetes RBAC: the
// dynamic client is built from their identity, so an unauthorized kind
// comes back as a real 403 from the API server. See AGENTS.md before
// widening or narrowing this.

// ErrUnknownKind is returned when the manifest names a Kind/apiVersion the
// cluster doesn't serve.
var ErrUnknownKind = errors.New("unknown kind")

// ApplyTarget is a Kind resolved to the REST resource the dynamic client
// needs.
type ApplyTarget struct {
	GVR        schema.GroupVersionResource
	Namespaced bool
}

// ResolveApplyTarget maps a Kind to its REST resource with one discovery
// call against that Kind's own group/version — cheaper than building a full
// RESTMapper per request, and never cached across requests, so a token-mode
// session's credentials can't outlive the request they belong to.
func ResolveApplyTarget(disc discovery.DiscoveryInterface, gvk schema.GroupVersionKind) (ApplyTarget, error) {
	list, err := disc.ServerResourcesForGroupVersion(gvk.GroupVersion().String())
	if apierrors.IsNotFound(err) {
		return ApplyTarget{}, fmt.Errorf("%w: apiVersion %s is not served by this cluster", ErrUnknownKind, gvk.GroupVersion())
	}
	if err != nil {
		return ApplyTarget{}, fmt.Errorf("discover %s: %w", gvk.GroupVersion(), err)
	}
	for _, r := range list.APIResources {
		if r.Kind != gvk.Kind || strings.Contains(r.Name, "/") {
			continue
		}
		return ApplyTarget{
			GVR:        gvk.GroupVersion().WithResource(r.Name),
			Namespaced: r.Namespaced,
		}, nil
	}
	return ApplyTarget{}, fmt.Errorf("%w: %q is not served under %s", ErrUnknownKind, gvk.Kind, gvk.GroupVersion())
}

func resourceClient(dyn dynamic.Interface, t ApplyTarget, namespace string) dynamic.ResourceInterface {
	if t.Namespaced {
		return dyn.Resource(t.GVR).Namespace(namespace)
	}
	return dyn.Resource(t.GVR)
}

func dryRunOpts(dryRun bool) []string {
	if dryRun {
		return []string{metav1.DryRunAll}
	}
	return nil
}

// GetLiveObject fetches the object's current state, for the update diff.
func GetLiveObject(ctx context.Context, dyn dynamic.Interface, t ApplyTarget, namespace, name string) (*unstructured.Unstructured, error) {
	obj, err := resourceClient(dyn, t, namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get %s %s/%s: %w", t.GVR.Resource, namespace, name, err)
	}
	return obj, nil
}

// CreateObject creates obj, or with dryRun set only asks the API server to
// validate it (schema + admission) and return what it would have stored.
func CreateObject(ctx context.Context, dyn dynamic.Interface, t ApplyTarget, obj *unstructured.Unstructured, dryRun bool) (*unstructured.Unstructured, error) {
	out, err := resourceClient(dyn, t, obj.GetNamespace()).Create(ctx, obj, metav1.CreateOptions{DryRun: dryRunOpts(dryRun), FieldValidation: "Strict"})
	if err != nil {
		return nil, fmt.Errorf("create %s: %w", t.GVR.Resource, err)
	}
	return out, nil
}

// UpdateObject replaces the existing object with obj (a PUT). If obj carries
// a metadata.resourceVersion the API server rejects the write with a 409 when
// it's stale — that's the editor's protection against clobbering a concurrent
// change.
func UpdateObject(ctx context.Context, dyn dynamic.Interface, t ApplyTarget, obj *unstructured.Unstructured, dryRun bool) (*unstructured.Unstructured, error) {
	out, err := resourceClient(dyn, t, obj.GetNamespace()).Update(ctx, obj, metav1.UpdateOptions{DryRun: dryRunOpts(dryRun), FieldValidation: "Strict"})
	if err != nil {
		return nil, fmt.Errorf("update %s %s/%s: %w", t.GVR.Resource, obj.GetNamespace(), obj.GetName(), err)
	}
	return out, nil
}
