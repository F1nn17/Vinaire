package gitops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
)

const (
	// fieldManager owns what Vinaire's GitOps applied, in server-side-apply terms.
	fieldManager = "vinaire-gitops"
	// SourceLabel marks every object a source applies, so the cluster itself says
	// where an object came from.
	SourceLabel = "vinaire.io/gitops-source"
)

// Applier checks and applies single objects against one cluster. The real one is
// a dynamic client acting as the source's ServiceAccount; tests substitute a fake.
type Applier interface {
	// Check compares an object with the cluster without changing anything.
	Check(ctx context.Context, obj *unstructured.Unstructured) ObjectStatus
	// Apply server-side-applies the object.
	Apply(ctx context.Context, obj *unstructured.Unstructured) error
	// Refresh forgets what the cluster's API discovery said, so custom resources
	// whose CRD was just created can be found.
	Refresh()
}

// dynamicApplier implements Applier.
type dynamicApplier struct {
	dyn       dynamic.Interface
	mapper    *restmapper.DeferredDiscoveryRESTMapper
	cache     discovery.CachedDiscoveryInterface
	defaultNS string
	sourceID  string
	force     bool
}

// NewApplier builds an Applier acting as cfg's identity.
func NewApplier(cfg *rest.Config, src Source) (Applier, error) {
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("building the dynamic client: %w", err)
	}
	dc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("building the discovery client: %w", err)
	}
	cache := memory.NewMemCacheClient(dc)
	ns := src.Namespace
	if ns == "" {
		ns = "default"
	}
	return &dynamicApplier{
		dyn: dyn, cache: cache, mapper: restmapper.NewDeferredDiscoveryRESTMapper(cache),
		defaultNS: ns, sourceID: src.ID, force: src.Force,
	}, nil
}

func (a *dynamicApplier) Refresh() {
	a.cache.Invalidate()
	a.mapper.Reset()
}

// prepare returns the object as it will be sent: a copy, in the right
// namespace, labelled with its source, without the fields that only the
// cluster may write (a manifest saved with `kubectl get -o yaml` has them).
func (a *dynamicApplier) prepare(obj *unstructured.Unstructured) (*unstructured.Unstructured, dynamic.ResourceInterface, error) {
	gvk := obj.GroupVersionKind()
	mapping, err := a.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, nil, fmt.Errorf("the cluster doesn't serve %s: %w", gvk, err)
	}
	u := obj.DeepCopy()
	for _, f := range []string{"status"} {
		delete(u.Object, f)
	}
	unstructured.RemoveNestedField(u.Object, "metadata", "resourceVersion")
	unstructured.RemoveNestedField(u.Object, "metadata", "uid")
	unstructured.RemoveNestedField(u.Object, "metadata", "creationTimestamp")
	unstructured.RemoveNestedField(u.Object, "metadata", "managedFields")
	unstructured.RemoveNestedField(u.Object, "metadata", "selfLink")
	unstructured.RemoveNestedField(u.Object, "metadata", "generation")

	labels := u.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	labels[SourceLabel] = a.sourceID
	u.SetLabels(labels)

	res := a.dyn.Resource(mapping.Resource)
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		ns := u.GetNamespace()
		if ns == "" {
			ns = a.defaultNS
			u.SetNamespace(ns)
		}
		return u, res.Namespace(ns), nil
	}
	u.SetNamespace("")
	return u, res, nil
}

func statusOf(obj *unstructured.Unstructured, state, msg string) ObjectStatus {
	ref := refOf(obj)
	return ObjectStatus{ObjectRef: ref, State: state, Message: msg}
}

func (a *dynamicApplier) patchOptions(dryRun bool) metav1.PatchOptions {
	f := a.force
	opts := metav1.PatchOptions{FieldManager: fieldManager, Force: &f}
	if dryRun {
		opts.DryRun = []string{metav1.DryRunAll}
	}
	return opts
}

// Check: not found = Missing; otherwise a server-side dry-run apply, and the
// object it would produce is compared with the live one. Comparing like this
// (rather than the manifest with the live object) is what lets the API server do
// the work of defaulting, merging and ownership, so a field the server fills in
// doesn't read as drift. A dry-run never changes the stored object, so its
// resourceVersion is the live one either way: the content is what is compared.
func (a *dynamicApplier) Check(ctx context.Context, obj *unstructured.Unstructured) ObjectStatus {
	u, res, err := a.prepare(obj)
	if err != nil {
		return statusOf(obj, StateError, err.Error())
	}
	st := statusOf(u, StateError, "")
	live, err := res.Get(ctx, u.GetName(), metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		st.State = StateMissing
		return st
	case err != nil:
		st.Message = apiMessage(err)
		return st
	}
	body, err := u.MarshalJSON()
	if err != nil {
		st.Message = err.Error()
		return st
	}
	dry, err := res.Patch(ctx, u.GetName(), types.ApplyPatchType, body, a.patchOptions(true))
	switch {
	case apierrors.IsConflict(err):
		st.State = StateOutOfSync
		st.Message = apiMessage(err) + " — enable \"take over\" on the source to let it adopt those fields"
		return st
	case err != nil:
		st.Message = apiMessage(err)
		return st
	}
	if sameContent(live, dry) {
		st.State = StateSynced
	} else {
		st.State = StateOutOfSync
	}
	return st
}

// sameContent reports whether applying would leave the object as it is, ignoring
// what only the server writes: bookkeeping metadata and status.
func sameContent(live, dry *unstructured.Unstructured) bool {
	return apiequality.Semantic.DeepEqual(normalized(live), normalized(dry))
}

func normalized(o *unstructured.Unstructured) map[string]any {
	c := o.DeepCopy()
	delete(c.Object, "status")
	for _, f := range []string{"resourceVersion", "managedFields", "generation", "selfLink"} {
		unstructured.RemoveNestedField(c.Object, "metadata", f)
	}
	return c.Object
}

func (a *dynamicApplier) Apply(ctx context.Context, obj *unstructured.Unstructured) error {
	u, res, err := a.prepare(obj)
	if err != nil {
		return err
	}
	body, err := u.MarshalJSON()
	if err != nil {
		return err
	}
	if _, err := res.Patch(ctx, u.GetName(), types.ApplyPatchType, body, a.patchOptions(false)); err != nil {
		msg := apiMessage(err)
		if apierrors.IsConflict(err) && !a.force {
			msg += " — enable \"take over\" on the source to let it overwrite fields other tools changed"
		}
		return errors.New(msg)
	}
	return nil
}

// apiMessage is the API server's own explanation, without client-go's wrapping.
func apiMessage(err error) string {
	var st apierrors.APIStatus
	if errors.As(err, &st) {
		if m := st.Status().Message; m != "" {
			return m
		}
	}
	return strings.TrimSpace(err.Error())
}
