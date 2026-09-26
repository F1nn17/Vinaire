package k8s

import (
	"context"
	"fmt"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// ListCRDs returns every CustomResourceDefinition installed on the cluster.
func ListCRDs(ctx context.Context, clientset *apiextensionsclientset.Clientset) ([]apiextensionsv1.CustomResourceDefinition, error) {
	list, err := clientset.ApiextensionsV1().CustomResourceDefinitions().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list customresourcedefinitions: %w", err)
	}
	return list.Items, nil
}

// GetCRD fetches a single CustomResourceDefinition by its full resource
// name (e.g. "gateways.gateway.networking.k8s.io").
func GetCRD(ctx context.Context, clientset *apiextensionsclientset.Clientset, name string) (*apiextensionsv1.CustomResourceDefinition, error) {
	crd, err := clientset.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get customresourcedefinition %s: %w", name, err)
	}
	return crd, nil
}

// CRDStorageVersion returns the version a CRD's instances are actually
// stored as (exactly one version always has Storage==true), falling back to
// the first declared version in the should-never-happen case none is
// marked.
func CRDStorageVersion(crd apiextensionsv1.CustomResourceDefinition) string {
	for _, v := range crd.Spec.Versions {
		if v.Storage {
			return v.Name
		}
	}
	if len(crd.Spec.Versions) > 0 {
		return crd.Spec.Versions[0].Name
	}
	return ""
}

// CRDGroupVersionResource builds the GVR a dynamic client needs to list/get
// a CRD's instances, using its storage version.
func CRDGroupVersionResource(crd apiextensionsv1.CustomResourceDefinition) schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    crd.Spec.Group,
		Version:  CRDStorageVersion(crd),
		Resource: crd.Spec.Names.Plural,
	}
}

// ListCRDInstances lists every instance of crd. If crd is namespace-scoped,
// namespace scopes the list (empty = all namespaces); a cluster-scoped CRD
// ignores namespace entirely.
func ListCRDInstances(ctx context.Context, dyn dynamic.Interface, crd apiextensionsv1.CustomResourceDefinition, namespace string) ([]unstructured.Unstructured, error) {
	gvr := CRDGroupVersionResource(crd)

	var (
		list *unstructured.UnstructuredList
		err  error
	)
	if crd.Spec.Scope == apiextensionsv1.NamespaceScoped {
		list, err = dyn.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
	} else {
		list, err = dyn.Resource(gvr).List(ctx, metav1.ListOptions{})
	}
	if err != nil {
		return nil, fmt.Errorf("list %s instances: %w", crd.Name, err)
	}
	return list.Items, nil
}

// GetCRDInstance fetches a single instance of crd by namespace and name.
// namespace is ignored for a cluster-scoped CRD.
func GetCRDInstance(ctx context.Context, dyn dynamic.Interface, crd apiextensionsv1.CustomResourceDefinition, namespace, name string) (*unstructured.Unstructured, error) {
	gvr := CRDGroupVersionResource(crd)

	var (
		obj *unstructured.Unstructured
		err error
	)
	if crd.Spec.Scope == apiextensionsv1.NamespaceScoped {
		obj, err = dyn.Resource(gvr).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	} else {
		obj, err = dyn.Resource(gvr).Get(ctx, name, metav1.GetOptions{})
	}
	if err != nil {
		return nil, fmt.Errorf("get %s instance %s: %w", crd.Name, name, err)
	}
	return obj, nil
}
